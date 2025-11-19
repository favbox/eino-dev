/*
 * stream.go - 流式数据处理核心实现
 *
 * 核心组件：
 *   - StreamReader/StreamWriter: 基于通道的流读写，支持异步数据传输
 *   - StreamReaderFromArray: 从数组创建流，支持统一的流式访问接口
 *   - MergeStreamReaders: 合并多个流，支持并发读取多个数据源
 *   - StreamReaderWithConvert: 流转换器，支持类型转换和数据过滤
 *   - MergeNamedStreamReaders: 命名流合并，支持追踪各源流的状态
 *
 * 设计特点：
 *   - 统一接口: 提供一致的 Recv/Close API，支持多种流实现
 *   - 零拷贝复制: Copy 方法创建独立读取器，共享底层数据
 *   - 自动资源管理: SetAutomaticClose 支持 GC 时自动清理
 *   - 错误追踪: SourceEOF 错误支持追踪命名流的结束状态
 *   - 并发安全: 支持多读取器并发访问同一流数据
 *   - 类型转换: StreamReaderWithConvert 支持链式转换和过滤
 */

package schema

import (
	"errors"
	"fmt"
	"io"
	"reflect"
	"runtime"
	"runtime/debug"
	"sync"
	"sync/atomic"

	"github.com/favbox/eino/internal/safe"
)

// ErrNoValue 在 StreamReaderWithConvert 中用于跳过某个 streamItem，将其从转换后的流中排除。
// 例如：
//
//	outStream = schema.StreamReaderWithConvert(s,
//		func(src string) (string, error) {
//			if len(src) == 0 {
//				return "", schema.ErrNoValue
//			}
//			return src.Message, nil
//		})
//
// outStream 将过滤掉空字符串。
//
// 注意：不要在其他情况下使用此错误。
var ErrNoValue = errors.New("no value")

// ErrRecvAfterClosed 表示在调用 StreamReader.Close 之后意外调用了 StreamReader.Recv。
// 此错误不应在正常使用 StreamReader.Recv 时出现。如果出现，请检查应用程序代码。
var ErrRecvAfterClosed = errors.New("recv after stream closed")

// SourceEOF 表示来自特定源流的 EOF 错误。
// 仅当使用 MergeNamedStreamReaders 创建的 StreamReader 的某个源流到达 EOF 时，
// 其 Recv() 方法才会返回此错误。
type SourceEOF struct {
	sourceName string
}

func (e *SourceEOF) Error() string {
	return fmt.Sprintf("EOF from source stream: %s", e.sourceName)
}

// GetSourceName 从 SourceEOF 错误中提取源流名称。
// 返回源流名称和一个布尔值，指示该错误是否为 SourceEOF。
// 如果错误不是 SourceEOF，则返回空字符串和 false。
func GetSourceName(err error) (string, bool) {
	var sErr *SourceEOF
	if errors.As(err, &sErr) {
		return sErr.sourceName, true
	}

	return "", false
}

// Pipe 创建一个具有给定容量的新流，由 StreamWriter 和 StreamReader 表示。
// 容量是流中可缓冲的最大项数。
// 例如：
//
//	sr, sw := schema.Pipe[string](3)
//	go func() { // 发送数据
//		defer sw.Close()
//		for i := 0; i < 10; i++ {
//			sw.Send(fmt.Sprintf("item_%d", i), nil)
//		}
//	}()
//
//	defer sr.Close()
//	for {
//		chunk, err := sr.Recv()
//		if errors.Is(err, io.EOF) {
//			break
//		}
//		fmt.Println(chunk)
//	}
func Pipe[T any](cap int) (*StreamReader[T], *StreamWriter[T]) {
	stm := newStream[T](cap)
	return stm.asReader(), &StreamWriter[T]{stm: stm}
}

// StreamWriter 是流的发送端。
// 通过 Pipe 函数创建。
// 例如：
//
//	sr, sw := schema.Pipe[string](3)
//	go func() { // 发送数据
//		defer sw.Close()
//		for i := 0; i < 10; i++ {
//			sw.Send(fmt.Sprintf("item_%d", i), nil)
//		}
//	}()
type StreamWriter[T any] struct {
	stm *stream[T]
}

// Send 向流发送一个值。
// 例如：
//
//	closed := sw.Send(item, nil)
//	if closed {
//		// 流已关闭
//	}
func (sw *StreamWriter[T]) Send(chunk T, err error) (closed bool) {
	return sw.stm.send(chunk, err)
}

// Close 通知接收端流发送已完成。
// 流接收端将从 StreamReader.Recv() 收到 io.EOF 错误。
// 注意：发送所有数据后务必调用 Close()。
// 例如：
//
//	defer sw.Close()
//	for i := 0; i < 10; i++ {
//		sw.Send(item, nil)
//	}
func (sw *StreamWriter[T]) Close() {
	sw.stm.closeSend()
}

// StreamReader 是流的接收端。
// 通过 Pipe 函数创建。
// 例如：
//
//	sr, sw := schema.Pipe[string](3)
//	// 省略发送数据的代码
//	// 大多数情况下，reader 由函数返回，并在另一个函数中使用。
//
//	defer sr.Close()
//	for {
//		chunk, err := sr.Recv()
//		if errors.Is(err, io.EOF) {
//			break
//		}
//		if err != nil {
//			// 处理错误
//		}
//		fmt.Println(chunk)
//	}
type StreamReader[T any] struct {
	typ readerType

	st *stream[T]

	ar *arrayReader[T]

	msr *multiStreamReader[T]

	srw *streamReaderWithConvert[T]

	csr *childStreamReader[T]
}

// Recv 从流接收一个值。
// 例如：
//
//	for {
//		chunk, err := sr.Recv()
//		if errors.Is(err, io.EOF) {
//			break
//		}
//		if err != nil {
//			// 处理错误
//		}
//		fmt.Println(chunk)
//	}
func (sr *StreamReader[T]) Recv() (T, error) {
	switch sr.typ {
	case readerTypeStream:
		return sr.st.recv()
	case readerTypeArray:
		return sr.ar.recv()
	case readerTypeMultiStream:
		return sr.msr.recv()
	case readerTypeWithConvert:
		return sr.srw.recv()
	case readerTypeChild:
		return sr.csr.recv()
	default:
		panic("impossible")
	}
}

// Close 安全地关闭 StreamReader。
// 应该只调用一次，因为多次调用可能无法按预期工作。
// 注意：使用 Recv() 后务必调用 Close()。
// 例如：
//
//	defer sr.Close()
//
//	for {
//		chunk, err := sr.Recv()
//		if errors.Is(err, io.EOF) {
//			break
//		}
//		fmt.Println(chunk)
//	}
func (sr *StreamReader[T]) Close() {
	switch sr.typ {
	case readerTypeStream:
		sr.st.closeRecv()
	case readerTypeArray:

	case readerTypeMultiStream:
		sr.msr.close()
	case readerTypeWithConvert:
		sr.srw.close()
	case readerTypeChild:
		sr.csr.close()
	default:
		panic("impossible")
	}
}

// Copy 创建新的 StreamReader 切片。
// 副本数量由参数 n 指定，应为非零正整数。
// 调用 Copy 后原始 StreamReader 将不可用。
// 例如：
//
//	sr := schema.StreamReaderFromArray([]int{1, 2, 3})
//	srs := sr.Copy(2)
//
//	sr1 := srs[0]
//	sr2 := srs[1]
//	defer sr1.Close()
//	defer sr2.Close()
//
//	chunk1, err1 := sr1.Recv()
//	chunk2, err2 := sr2.Recv()
func (sr *StreamReader[T]) Copy(n int) []*StreamReader[T] {
	if n < 2 {
		return []*StreamReader[T]{sr}
	}

	if sr.typ == readerTypeArray {
		ret := make([]*StreamReader[T], n)
		for i, ar := range sr.ar.copy(n) {
			ret[i] = &StreamReader[T]{typ: readerTypeArray, ar: ar}
		}
		return ret
	}

	return copyStreamReaders[T](sr, n)
}

// SetAutomaticClose 设置 StreamReader 在不再可达且准备被 GC 时自动关闭。
// 非并发安全。
func (sr *StreamReader[T]) SetAutomaticClose() {
	switch sr.typ {
	case readerTypeStream:
		if !sr.st.automaticClose {
			sr.st.automaticClose = true
			var flag uint32
			sr.st.closedFlag = &flag
			runtime.SetFinalizer(sr, func(s *StreamReader[T]) {
				s.Close()
			})
		}
	case readerTypeMultiStream:
		for _, s := range sr.msr.nonClosedStreams() {
			if !s.automaticClose {
				s.automaticClose = true
				var flag uint32
				s.closedFlag = &flag
				runtime.SetFinalizer(s, func(st *stream[T]) {
					st.closeRecv()
				})
			}
		}
	case readerTypeChild:
		parent := sr.csr.parent.sr
		parent.SetAutomaticClose()
	case readerTypeWithConvert:
		sr.srw.sr.SetAutomaticClose()
	case readerTypeArray:
		// no need to clean up
	default:
	}
}

func (sr *StreamReader[T]) recvAny() (any, error) {
	return sr.Recv()
}

func (sr *StreamReader[T]) copyAny(n int) []iStreamReader {
	ret := make([]iStreamReader, n)

	srs := sr.Copy(n)

	for i := 0; i < n; i++ {
		ret[i] = srs[i]
	}

	return ret
}

func arrToStream[T any](arr []T) *stream[T] {
	s := newStream[T](len(arr))
	for i := range arr {
		s.send(arr[i], nil)
	}
	s.closeSend()

	return s
}

func (sr *StreamReader[T]) toStream() *stream[T] {
	switch sr.typ {
	case readerTypeStream:
		return sr.st
	case readerTypeArray:
		return sr.ar.toStream()
	case readerTypeMultiStream:
		return sr.msr.toStream()
	case readerTypeWithConvert:
		return sr.srw.toStream()
	case readerTypeChild:
		return sr.csr.toStream()
	default:
		panic("impossible")
	}
}

type readerType int

const (
	readerTypeStream readerType = iota
	readerTypeArray
	readerTypeMultiStream
	readerTypeWithConvert
	readerTypeChild
)

type iStreamReader interface {
	recvAny() (any, error)
	copyAny(int) []iStreamReader
	Close()
	SetAutomaticClose()
}

// stream 是基于通道的流，具有 1 个发送者和 1 个接收者。
// 发送者调用 closeSend() 通知接收者流发送已完成。
// 接收者调用 closeRecv() 通知发送者接收者停止接收。
type stream[T any] struct {
	items chan streamItem[T]

	closed chan struct{}

	automaticClose bool
	closedFlag     *uint32 // 0 = not closed, 1 = closed, only used when automaticClose is set
}

type streamItem[T any] struct {
	chunk T
	err   error
}

func newStream[T any](cap int) *stream[T] {
	return &stream[T]{
		items:  make(chan streamItem[T], cap),
		closed: make(chan struct{}),
	}
}

func (s *stream[T]) asReader() *StreamReader[T] {
	return &StreamReader[T]{typ: readerTypeStream, st: s}
}

func (s *stream[T]) recv() (chunk T, err error) {
	item, ok := <-s.items

	if !ok {
		item.err = io.EOF
	}

	return item.chunk, item.err
}

func (s *stream[T]) send(chunk T, err error) (closed bool) {
	// if the stream is closed, return immediately
	select {
	case <-s.closed:
		return true
	default:
	}

	item := streamItem[T]{chunk, err}

	select {
	case <-s.closed:
		return true
	case s.items <- item:
		return false
	}
}

func (s *stream[T]) closeSend() {
	close(s.items)
}

func (s *stream[T]) closeRecv() {
	if s.automaticClose {
		if atomic.CompareAndSwapUint32(s.closedFlag, 0, 1) {
			close(s.closed)
		}
		return
	}

	close(s.closed)
}

// StreamReaderFromArray 从给定的元素切片创建 StreamReader。
// 接受类型 T 的数组，返回指向 StreamReader[T] 的指针。
// 这允许以受控方式流式传输数组元素。
// 例如：
//
//	sr := schema.StreamReaderFromArray([]int{1, 2, 3})
//	defer sr.Close()
//
//	for {
//		chunk, err := sr.Recv()
//		if errors.Is(err, io.EOF) {
//			break
//		}
//		fmt.Println(chunk)
//	}
func StreamReaderFromArray[T any](arr []T) *StreamReader[T] {
	return &StreamReader[T]{ar: &arrayReader[T]{arr: arr}, typ: readerTypeArray}
}

type arrayReader[T any] struct {
	arr   []T
	index int
}

func (ar *arrayReader[T]) recv() (T, error) {
	if ar.index < len(ar.arr) {
		ret := ar.arr[ar.index]
		ar.index++

		return ret, nil
	}

	var t T
	return t, io.EOF
}

func (ar *arrayReader[T]) copy(n int) []*arrayReader[T] {
	ret := make([]*arrayReader[T], n)

	for i := 0; i < n; i++ {
		ret[i] = &arrayReader[T]{
			arr:   ar.arr,
			index: ar.index,
		}
	}

	return ret
}

func (ar *arrayReader[T]) toStream() *stream[T] {
	return arrToStream(ar.arr[ar.index:])
}

type multiArrayReader[T any] struct {
	ars   []*arrayReader[T]
	index int
}

type multiStreamReader[T any] struct {
	sts []*stream[T]

	itemsCases []reflect.SelectCase

	nonClosed []int

	sourceReaderNames []string
}

func newMultiStreamReader[T any](sts []*stream[T]) *multiStreamReader[T] {
	var itemsCases []reflect.SelectCase
	if len(sts) > maxSelectNum {
		itemsCases = make([]reflect.SelectCase, len(sts))
		for i, st := range sts {
			itemsCases[i] = reflect.SelectCase{
				Dir:  reflect.SelectRecv,
				Chan: reflect.ValueOf(st.items),
			}
		}
	}

	nonClosed := make([]int, len(sts))
	for i := range sts {
		nonClosed[i] = i
	}

	return &multiStreamReader[T]{
		sts:        sts,
		itemsCases: itemsCases,
		nonClosed:  nonClosed,
	}
}

func (msr *multiStreamReader[T]) recv() (T, error) {
	for len(msr.nonClosed) > 0 {
		var chosen int
		var ok bool
		if len(msr.nonClosed) > maxSelectNum {
			var recv reflect.Value
			chosen, recv, ok = reflect.Select(msr.itemsCases)
			if ok {
				item := recv.Interface().(streamItem[T])
				return item.chunk, item.err
			}
			msr.itemsCases[chosen].Chan = reflect.Value{}
		} else {
			var item *streamItem[T]
			chosen, item, ok = receiveN(msr.nonClosed, msr.sts)
			if ok {
				return item.chunk, item.err
			}
		}

		// delete the closed stream
		for i := range msr.nonClosed {
			if msr.nonClosed[i] == chosen {
				msr.nonClosed = append(msr.nonClosed[:i], msr.nonClosed[i+1:]...)
				break
			}
		}

		if len(msr.sourceReaderNames) > 0 {
			var t T
			return t, &SourceEOF{msr.sourceReaderNames[chosen]}
		}
	}

	var t T
	return t, io.EOF
}

func (msr *multiStreamReader[T]) nonClosedStreams() []*stream[T] {
	ret := make([]*stream[T], len(msr.nonClosed))

	for i, idx := range msr.nonClosed {
		ret[i] = msr.sts[idx]
	}

	return ret
}

func (msr *multiStreamReader[T]) close() {
	for _, s := range msr.sts {
		s.closeRecv()
	}
}

func (msr *multiStreamReader[T]) toStream() *stream[T] {
	return toStream[T, *multiStreamReader[T]](msr)
}

type streamReaderWithConvert[T any] struct {
	sr iStreamReader

	convert func(any) (T, error)
}

func newStreamReaderWithConvert[T any](origin iStreamReader, convert func(any) (T, error)) *StreamReader[T] {
	srw := &streamReaderWithConvert[T]{
		sr:      origin,
		convert: convert,
	}

	return &StreamReader[T]{
		typ: readerTypeWithConvert,
		srw: srw,
	}
}

// StreamReaderWithConvert 将流读取器转换为另一个流读取器。
//
// 例如：
//
//	intReader := StreamReaderFromArray([]int{1, 2, 3})
//	stringReader := StreamReaderWithConvert(intReader, func(i int) (string, error) {
//		return fmt.Sprintf("val_%d", i), nil
//	})
//
//	defer stringReader.Close() // 如果使用 Recv()，务必关闭 reader，否则可能导致内存/协程泄漏。
//	s, err := stringReader.Recv()
//	fmt.Println(s) // 输出: val_1
func StreamReaderWithConvert[T, D any](sr *StreamReader[T], convert func(T) (D, error)) *StreamReader[D] {
	c := func(a any) (D, error) {
		return convert(a.(T))
	}

	return newStreamReaderWithConvert(sr, c)
}

func (srw *streamReaderWithConvert[T]) recv() (T, error) {
	for {
		out, err := srw.sr.recvAny()

		if err != nil {
			var t T
			return t, err
		}

		t, err := srw.convert(out)
		if err == nil {
			return t, nil
		}

		if !errors.Is(err, ErrNoValue) {
			return t, err
		}
	}
}

func (srw *streamReaderWithConvert[T]) close() {
	srw.sr.Close()
}

type reader[T any] interface {
	recv() (T, error)
	close()
}

func toStream[T any, Reader reader[T]](r Reader) *stream[T] {
	ret := newStream[T](5)

	go func() {
		defer func() {
			panicErr := recover()
			if panicErr != nil {
				e := safe.NewPanicErr(panicErr, debug.Stack())

				var chunk T
				_ = ret.send(chunk, e)
			}

			ret.closeSend()
			r.close()
		}()

		for {
			out, err := r.recv()
			if err == io.EOF {
				break
			}

			closed := ret.send(out, err)
			if closed {
				break
			}
		}
	}()

	return ret
}

func (srw *streamReaderWithConvert[T]) toStream() *stream[T] {
	return toStream[T, *streamReaderWithConvert[T]](srw)
}

type cpStreamElement[T any] struct {
	once sync.Once
	next *cpStreamElement[T]
	item streamItem[T]
}

// copyStreamReaders 从单个 StreamReader 创建多个独立的 StreamReader。
// 每个子 StreamReader 可以独立地从原始流读取。
func copyStreamReaders[T any](sr *StreamReader[T], n int) []*StreamReader[T] {
	cpsr := &parentStreamReader[T]{
		sr:            sr,
		subStreamList: make([]*cpStreamElement[T], n),
		closedNum:     0,
	}

	// 使用空元素初始化 subStreamList，该元素充当尾节点。
	// nil 元素（用于解引用）表示子流已关闭。
	// 当原始通道的长度未知时，链接前一个和当前元素具有挑战性。
	// 此外，使用前向指针会使元素解引用变得复杂，可能需要引用计数。
	elem := &cpStreamElement[T]{}

	for i := range cpsr.subStreamList {
		cpsr.subStreamList[i] = elem
	}

	ret := make([]*StreamReader[T], n)
	for i := range ret {
		ret[i] = &StreamReader[T]{
			csr: &childStreamReader[T]{
				parent: cpsr,
				index:  i,
			},
			typ: readerTypeChild,
		}
	}

	return ret
}

type parentStreamReader[T any] struct {
	// sr 是原始 StreamReader。
	sr *StreamReader[T]

	// subStreamList 将每个子流的索引映射到其最新读取的块。
	// 每个值来自 cpStreamElement 的隐藏链表。
	subStreamList []*cpStreamElement[T]

	// closedNum 是已关闭子流的计数。
	closedNum uint32
}

// peek 对相同 idx 的并发使用不安全，但对不同 idx 是安全的。
// 确保每个子 StreamReader 在单个 goroutine 中使用 for 循环。
func (p *parentStreamReader[T]) peek(idx int) (t T, err error) {
	elem := p.subStreamList[idx]
	if elem == nil {
		// 子流关闭后意外调用接收。
		return t, ErrRecvAfterClosed
	}

	// 这里的 sync.Once 用于：
	// 1. 写入此 cpStreamElement 的内容。
	// 2. 使用空的 cpStreamElement 初始化此 cpStreamElement 的 'next' 字段，
	//    类似于 copyStreamReaders 中的初始化。
	elem.once.Do(func() {
		t, err = p.sr.Recv()
		elem.item = streamItem[T]{chunk: t, err: err}
		if err != io.EOF {
			elem.next = &cpStreamElement[T]{}
			p.subStreamList[idx] = elem.next
		}
	})

	// 元素已设置，不会再次修改。
	// 因此，子流可以并发读取此元素的内容和 'next' 指针。
	t = elem.item.chunk
	err = elem.item.err
	if err != io.EOF {
		p.subStreamList[idx] = elem.next
	}

	return t, err
}

func (p *parentStreamReader[T]) close(idx int) {
	if p.subStreamList[idx] == nil {
		return // 避免多次关闭
	}

	p.subStreamList[idx] = nil

	curClosedNum := atomic.AddUint32(&p.closedNum, 1)

	allClosed := int(curClosedNum) == len(p.subStreamList)
	if allClosed {
		p.sr.Close()
	}
}

type childStreamReader[T any] struct {
	parent *parentStreamReader[T]
	index  int
}

func (csr *childStreamReader[T]) recv() (T, error) {
	return csr.parent.peek(csr.index)
}

func (csr *childStreamReader[T]) toStream() *stream[T] {
	return toStream[T, *childStreamReader[T]](csr)
}

func (csr *childStreamReader[T]) close() {
	csr.parent.close(csr.index)
}

// MergeStreamReaders 将多个 StreamReader 合并为一个。
// 当需要将多个流合并为一个时非常有用。
// 例如：
//
//	sr1, sw1 := schema.Pipe[string](2)
//	sr2, sw2 := schema.Pipe[string](2)
//	// ... 发送数据到 sw1 和 sw2 ...
//
//	sr := schema.MergeStreamReaders([]*schema.StreamReader[string]{sr1, sr2})
//
//	defer sr.Close()
//	for {
//		chunk, err := sr.Recv()
//		if errors.Is(err, io.EOF) {
//			break
//		}
//		fmt.Println(chunk)
//	}
func MergeStreamReaders[T any](srs []*StreamReader[T]) *StreamReader[T] {
	if len(srs) < 1 {
		return nil
	}

	if len(srs) < 2 {
		return srs[0]
	}

	var arr []T
	var ss []*stream[T]

	for _, sr := range srs {
		switch sr.typ {
		case readerTypeStream:
			ss = append(ss, sr.st)
		case readerTypeArray:
			arr = append(arr, sr.ar.arr[sr.ar.index:]...)
		case readerTypeMultiStream:
			ss = append(ss, sr.msr.nonClosedStreams()...)
		case readerTypeWithConvert:
			ss = append(ss, sr.srw.toStream())
		case readerTypeChild:
			ss = append(ss, sr.csr.toStream())
		default:
			panic("impossible")
		}
	}

	if len(ss) == 0 {
		return &StreamReader[T]{
			typ: readerTypeArray,
			ar: &arrayReader[T]{
				arr:   arr,
				index: 0,
			},
		}
	}

	if len(arr) != 0 {
		s := arrToStream(arr)
		ss = append(ss, s)
	}

	return &StreamReader[T]{
		typ: readerTypeMultiStream,
		msr: newMultiStreamReader(ss),
	}
}

// MergeNamedStreamReaders 将多个 StreamReader 合并为一个，并保留它们的名称。
// 当源流到达 EOF 时，合并后的流将返回包含结束的源流名称的 SourceEOF 错误。
// 当需要追踪哪个源流已完成时非常有用。
// 例如：
//
//	sr1, sw1 := schema.Pipe[string](2)
//	sr2, sw2 := schema.Pipe[string](2)
//
//	namedStreams := map[string]*StreamReader[string]{
//		"stream1": sr1,
//		"stream2": sr2,
//	}
//
//	mergedSR := schema.MergeNamedStreamReaders(namedStreams)
//	defer mergedSR.Close()
//
//	for {
//		chunk, err := mergedSR.Recv()
//		if err != nil {
//			if sourceName, ok := schema.GetSourceName(err); ok {
//				fmt.Printf("流 %s 已结束\n", sourceName)
//				continue
//			}
//			if errors.Is(err, io.EOF) {
//				break // 所有流都已结束
//			}
//			// 处理其他错误
//		}
//		fmt.Println(chunk)
//	}
func MergeNamedStreamReaders[T any](srs map[string]*StreamReader[T]) *StreamReader[T] {
	if len(srs) < 1 {
		return nil
	}

	ss := make([]*StreamReader[T], len(srs))
	names := make([]string, len(srs))

	i := 0
	for name, sr := range srs {
		ss[i] = sr
		names[i] = name
		i++
	}

	return InternalMergeNamedStreamReaders(ss, names)
}

func InternalMergeNamedStreamReaders[T any](srs []*StreamReader[T], names []string) *StreamReader[T] {
	ss := make([]*stream[T], len(srs))

	for i, sr := range srs {
		ss[i] = sr.toStream()
	}

	msr := newMultiStreamReader(ss)
	msr.sourceReaderNames = names

	return &StreamReader[T]{
		typ: readerTypeMultiStream,
		msr: msr,
	}
}
