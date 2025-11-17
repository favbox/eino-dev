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

// === Layer 1: 公开API层 (用户立即看到) ===

// ========================================
// Layer 1: 公开API层 (用户立即看到)
// ========================================

// 1.1 创建类函数

// Pipe 创建指定容量的流，返回流写入器和流读取器。
// 容量表示流中可缓冲的最大数据项数量。
//
// 示例:
//
//	sr, sw := schema.Pipe[string](3)
//	go func() { // 发送数据
//	        defer sw.Close()
//	        for i := 0; i < 10; i++ {
//	                sw.Send(i, nil)
//	        }
//	}
//
//	defer sr.Close()
//	for chunk, err := sr.Recv() {
//	        if errors.Is(err, io.EOF) {
//	                break
//	        }
//	        fmt.Println(chunk)
//	}
func Pipe[T any](cap int) (*StreamReader[T], *StreamWriter[T]) {
	stm := newStream[T](cap)
	return stm.asReader(), &StreamWriter[T]{stm: stm}
}

// StreamReaderFromArray 从给定数组创建流读取器。
// 允许以流的方式控制数组元素的读取。
//
// 示例：
//
//	sr := schema.StreamReaderFromArray([]int{1, 2, 3})
//	defer sr.close()
//
//	for chunk, err := sr.Recv() {
//		fmt.Println(chunk)
//	}
func StreamReaderFromArray[T any](arr []T) *StreamReader[T] {
	return &StreamReader[T]{ar: &arrayReader[T]{arr: arr}, typ: readerTypeArray}
}

// 1.2 合并类函数

// MergeStreamReaders 合并多个流读取器为一个。
// 将多个数据源统一为单一的流接口，便于并发处理。
//
// 示例:
//
//	sr1, sr2 := schema.Pipe[string](2)
//	defer sr1.Close()
//	defer sr2.Close()
//
//	sr := schema.MergeStreamReaders([]*schema.StreamReader[string]{sr1, sr2})
//
//	defer sr.Close()
//	for chunk, err := sr.Recv() {
//	        fmt.Println(chunk)
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

	// 根据读取器类型分别处理
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
			panic("不可能的情况")
		}
	}

	// 只有数组类型，直接返回数组读取器
	if len(ss) == 0 {
		return &StreamReader[T]{
			typ: readerTypeArray,
			ar: &arrayReader[T]{
				arr:   arr,
				index: 0,
			},
		}
	}

	// 同时存在数组和其他类型，将数组转为流后合并
	if len(arr) != 0 {
		s := arrToStream(arr)
		ss = append(ss, s)
	}

	return &StreamReader[T]{
		typ: readerTypeMultiStream,
		msr: newMultiStreamReader(ss),
	}
}

// MergeNamedStreamReaders 合并多个命名流读取器，保留源流名称。
// 当某个源流结束时，合并流将返回包含该源流名称的 SourceEOF 错误。
// 适用于需要跟踪哪个源流已完成的场景。
//
// 示例:
//
//	sr1, sw1 := schema.Pipe[string](2)
//	sr2, sw2 := schema.Pipe[string](2)
//
//	namedStreams := map[string]*StreamReader[string]{
//	        "stream1": sr1,
//	        "stream2": sr2,
//	}
//
//	mergedSR := schema.MergeNamedStreamReaders(namedStreams)
//	defer mergedSR.Close()
//
//	for {
//	        chunk, err := mergedSR.Recv()
//	        if err != nil {
//	                if sourceName, ok := schema.GetSourceName(err); ok {
//	                        fmt.Printf("流 %s 已结束\n", sourceName)
//	                        continue
//	                }
//	                if errors.Is(err, io.EOF) {
//	                        break // 所有流都已结束
//	                }
//	                // 处理其他错误
//	        }
//	        fmt.Println(chunk)
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

// InternalMergeNamedStreamReaders 内部命名流合并函数。
// 将流读取器和名称列表转换为命名多流读取器。
func InternalMergeNamedStreamReaders[T any](srs []*StreamReader[T], names []string) *StreamReader[T] {
	ss := make([]*stream[T], len(srs))

	// 将所有流读取器转换为底层流对象
	for i, sr := range srs {
		ss[i] = sr.toStream()
	}

	// 创建多流读取器并设置源流名称
	msr := newMultiStreamReader(ss)
	msr.sourceReaderNames = names

	return &StreamReader[T]{
		typ: readerTypeMultiStream,
		msr: msr,
	}
}

// 1.3 转换类函数

// StreamReaderWithConvert 将流读取器转换为另一种类型的流读取器。
//
// 示例：
//
//	intReader := StreamReaderFromArray([]int{1, 2, 3})
//	stringReader := StreamReaderWithConvert(sr, func(i int) (string, error) {
//		return fmt.Sprintf("val_%d", i), nil
//	})
//
// defer stringReader.Close() // 使用 Recv() 时必须关闭，否则可能导致内存/协程泄露
//
//	s, err := stringReader.Recv()
//	fmt.Println(s) // 输出：val_1
func StreamReaderWithConvert[T, D any](sr *StreamReader[T], convert func(T) (D, error)) *StreamReader[D] {
	c := func(a any) (D, error) {
		return convert(a.(T))
	}

	return newStreamReaderWithConvert(sr, c)
}

// 1.4 工具类函数

// GetSourceName 从 SourceEOF 错误中提取源流名称。
// 返回源流名称和是否为 SourceEOF 错误的布尔值。
// 非SourceEOF 错误返回空字符串和 false。
func GetSourceName(err error) (string, bool) {
	var sErr *SourceEOF
	if errors.As(err, &sErr) {
		return sErr.sourceName, true
	}

	return "", false
}

// === Layer 2: 核心类型定义层 ===

// 2.1 核心接口

// reader 流读取器接口，定义了基本的读取和关闭操作。
type reader[T any] interface {
	recv() (T, error) // 接收数据
	close()           // 关闭读取器
}

// iStreamReader 内部流读取器接口，使用 any 类型支持异构数据处理。
type iStreamReader interface {
	recvAny() (any, error)       // 接收任意类型的数据
	copyAny(int) []iStreamReader // 创建指定数量的读取器副本
	Close()                      // 关闭读取器
	SetAutomaticClose()          // 设置自动关闭模式
}

// 2.2 核心结构体

// StreamReader 流数据接收器。
// 由 Pipe 函数创建，用于从流中读取数据。
//
// 示例：
//
//	sr, sw := schema.Pipe[string](3)
//	// 发送数据的代码省略
//	// 通常 reader 由函数返回，在另一个函数中使用
//
//	for chunk, err := sr.Recv() {
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

// StreamWriter 流数据发送器。
// 由 Pipe 函数创建，用于向流中发送数据。
//
// 示例:
//
//	sr, sw := schema.Pipe[string](3)
//	go func() { // 发送数据
//	        defer sw.Close()
//	        for i := 0; i < 10; i++ {
//	                sw.Send(i, nil)
//	        }
//	}
type StreamWriter[T any] struct {
	stm *stream[T] // 底层流对象
}

// === Layer 3: 类型方法层 ===

// 3.1 StreamReader 方法 (按使用频率排序)

// Recv 从流中接收数据。
// 根据读取器类型调用相应的接收方法。
//
// 示例:
//
//	for chunk, err := sr.Recv() {
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
		panic("不可能的情况")
	}
}

// Close 安全关闭流读取器。
// 只应调用一次，多次调用可能导致意外行为。
// 注意：使用 Recv() 后务必记得调用 Close()。
//
// 示例:
//
//	defer sr.Close()
//
//	for chunk, err := sr.Recv() {
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
		// 数组读取器无需关闭
	case readerTypeMultiStream:
		sr.msr.close()
	case readerTypeWithConvert:
		sr.srw.close()
	case readerTypeChild:
		sr.csr.close()
	default:
		panic("不可能的情况")
	}
}

// Copy 复制流读取器，使多个消费者能同时读取同一数据源。
// 原始流读取器复制后将不可用。
//
// 应用场景：并发处理、数据分发、流水线处理
//
// 示例:
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

// SetAutomaticClose 设置流读取器为自动关闭模式。
// 当流读取器不可达且准备被 GC 回收时自动关闭。
// 注意：非并发安全。
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
		// 数组流无需自动清理
	default:
	}
}

// recvAny 以 any 类型接收数据。
// 内部调用泛型版本的 Recv() 方法。
func (sr *StreamReader[T]) recvAny() (any, error) {
	return sr.Recv()
}

// copyAny 以 iStreamReader 接口类型创建流读取器副本。
// 内部调用泛型版本的 Copy() 方法并转换为接口类型。
func (sr *StreamReader[T]) copyAny(n int) []iStreamReader {
	ret := make([]iStreamReader, n)

	srs := sr.Copy(n)

	for i := 0; i < n; i++ {
		ret[i] = srs[i]
	}

	return ret
}

// toStream 将流读取器转换为底层流对象。
// 根据读取器类型调用相应的转换方法。
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
		panic("不可能的情况")
	}
}

// 3.2 StreamWriter 方法

// Send 向流中发送数据。
// 返回值表示流是否已关闭。
//
// 示例:
//
//	closed := sw.Send(i, nil)
//	if closed {
//	        // 流已关闭
//	}
func (sw *StreamWriter[T]) Send(chunk T, err error) (closed bool) {
	return sw.stm.send(chunk, err)
}

// Close 关闭流的发送端，通知接收者发送已完成。
// 接收者将从 StreamReader.Recv() 收到 io.EOF 错误。
// 注意：发送完所有数据后务必记得调用 Close()。
//
// 示例:
//
//	defer sw.Close()
//	for i := 0; i < 10; i++ {
//	        sw.Send(i, nil)
//	}
func (sw *StreamWriter[T]) Close() {
	sw.stm.closeSend()
}

// === Layer 4: 内部实现层 ===

// 4.1 错误和常量定义

// ErrNoValue 用于 StreamReaderWithConvert 中跳过流数据项。
// 在转换函数中返回此错误会从转换后的流中排除该项。
//
// 示例：
//
//	outStream := schema.StreamReaderWithConvert(s,
//		func(src string) (string, error) {
//			if len(src) == 0 {
//				return "", schema.ErrNoValue
//			}
//
//			return src.Message, nil
//		})
//
//	outStream 将过滤掉空字符串。
//
// 请勿在其他情况下使用。
var ErrNoValue = errors.New("无有效值")

// ErrRecvAfterClosed 表示在流关闭后意外调用了接收操作。
// 正常使用情况下不应出现此错误，如出现请检查应用代码。
var ErrRecvAfterClosed = errors.New("流关闭后接收数据")

// SourceEOF 表示特定源流结束的错误。
// 仅在 MergeNamedStreamReaders 创建的 StreamReader 的 Recv() 方法中返回。
type SourceEOF struct {
	sourceName string // 源流名称
}

func (e *SourceEOF) Error() string {
	return fmt.Sprintf("源流 %s 已结束", e.sourceName)
}

// readerType 表示流读取器的类型
type readerType int

const (
	readerTypeStream      readerType = iota // 基础流读取器
	readerTypeArray                         // 数组读取器
	readerTypeMultiStream                   // 多流读取器
	readerTypeWithConvert                   // 带转换的读取器
	readerTypeChild
)

// 4.2 底层流实现

// stream 基于 channel 的底层流，支持 1 个发送者和 1 个接收者。
// 发送者调用 closeSend() 通知接收者流已结束，接收者调用 closeRecv() 通知发送者停止接收。
type stream[T any] struct {
	items chan streamItem[T] // 数据传输通道

	closed chan struct{} // 关闭信号通道

	automaticClose bool    // 是否启用自动关闭模式
	closedFlag     *uint32 // 关闭标志：0=未关闭，1=已关闭（仅在自动关闭模式下使用）
}

// streamItem 流中的数据项，包含数据块和可能的错误。
type streamItem[T any] struct {
	chunk T
	err   error
}

// stream 方法

// newStream 创建指定容量的新流。
func newStream[T any](cap int) *stream[T] {
	return &stream[T]{
		items:  make(chan streamItem[T], cap),
		closed: make(chan struct{}),
	}
}

// asReader 将流转换为流读取器。
func (s *stream[T]) asReader() *StreamReader[T] {
	return &StreamReader[T]{typ: readerTypeStream, st: s}
}

// recv 从流中接收数据块。
// 流已关闭：返回 io.EOF 错误；正常情况：返回数据块和可能的错误。
func (s *stream[T]) recv() (chunk T, err error) {
	item, ok := <-s.items

	if !ok {
		item.err = io.EOF
	}

	return item.chunk, item.err
}

// send 向流中发送数据块。
// 流已关闭：返回 true；发送成功：返回 false
func (s *stream[T]) send(chunk T, err error) (closed bool) {
	// 流已关闭时立即返回
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

// closeSend 关闭流的发送端，通知接收者流已结束。
func (s *stream[T]) closeSend() {
	close(s.items)
}

// closeRecv 关闭流的接收端，通知发送者停止接收。
// 自动关闭模式：使用原子操作确保只关闭一次；普通模式：直接关闭
func (s *stream[T]) closeRecv() {
	if s.automaticClose {
		if atomic.CompareAndSwapUint32(s.closedFlag, 0, 1) {
			close(s.closed)
		}
		return
	}

	close(s.closed)
}

// 4.3 具体读取器实现

// 基于数组的读取器，顺序读取数组元素
type arrayReader[T any] struct {
	arr   []T // 源数组
	index int // 当前读取位置
}

// arrayReader 方法

// recv 从数组中读取下一个元素。
// 有元素：返回元素和 nil；无元素：返回零值和 io.EOF
func (ar *arrayReader[T]) recv() (T, error) {
	if ar.index < len(ar.arr) {
		ret := ar.arr[ar.index]
		ar.index++

		return ret, nil
	}

	var t T
	return t, io.EOF
}

// copy 创建指定数量的读取器副本。
// 所有副本共享源数组，但维护独立的读取位置
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

// toStream 将剩余数组元素转换为流。
// 从当前读取位置开始创建新的流
func (ar *arrayReader[T]) toStream() *stream[T] {
	return arrToStream(ar.arr[ar.index:])
}

// multiStreamReader 多流读取器，同时从多个流中读取数据。
type multiStreamReader[T any] struct {
	sts               []*stream[T]         // 源流列表
	itemsCases        []reflect.SelectCase // reflect.Select 的 case 列表
	nonClosed         []int                // 未关闭流的索引列表
	sourceReaderNames []string             // 源读取器名称列表
}

// multiStreamReader 方法

// newMultiStreamReader 创建多流读取器。
// 流数量超过阈值时使用 reflect.Select 优化性能
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

// recv 从多个流中读取数据。
// 有数据：返回数据块和 nil；流关闭：移除该流；所有流关闭：返回 io.EOF
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

		// 移除已关闭的流
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

// nonClosedStreams 获取未关闭的流列表。
func (msr *multiStreamReader[T]) nonClosedStreams() []*stream[T] {
	ret := make([]*stream[T], len(msr.nonClosed))

	for i, idx := range msr.nonClosed {
		ret[i] = msr.sts[idx]
	}

	return ret
}

// close 关闭所有流的接收端。
func (msr *multiStreamReader[T]) close() {
	for _, s := range msr.sts {
		s.closeRecv()
	}
}

// toStream 将多流读取器转换为流。
func (msr *multiStreamReader[T]) toStream() *stream[T] {
	return toStream[T, *multiStreamReader[T]](msr)
}

// streamReaderWithConvert 带转换功能的流读取器。
// 将原始流数据通过转换函数转换为目标类型
type streamReaderWithConvert[T any] struct {
	sr      iStreamReader        // 原始流读取器
	convert func(any) (T, error) // 转换函数
}

// streamReaderWithConvert 方法

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

// recv 接收并转换流数据。
// 转换成功：返回转换后的数据；转换失败：处理错误并继续尝试
func (srw *streamReaderWithConvert[T]) recv() (T, error) {
	for {
		out, err := srw.sr.recvAny()

		if err != nil {
			var t T
			return t, err
		}

		t, err := srw.convert(out)
		if err == nil {
			return t, err
		}

		if !errors.Is(err, ErrNoValue) {
			return t, err
		}
	}
}

// close 关闭原始流读取器。
func (srw *streamReaderWithConvert[T]) close() {
	srw.sr.Close()
}

// toStream 将带转换功能的读取器转换为流。
func (srw *streamReaderWithConvert[T]) toStream() *stream[T] {
	return toStream[T, *streamReaderWithConvert[T]](srw)
}

// parentStreamReader 父流读取器，管理多个子流读取器的数据分发。
type parentStreamReader[T any] struct {
	// sr 原始流读取器
	sr *StreamReader[T]

	// subStreamList 将每个子流索引映射到其最新读取的数据块
	// 每个值来自隐藏的 cpStreamElement 链表
	subStreamList []*cpStreamElement[T]

	// closedNum 已关闭的子流数量
	closedNum uint32
}

// childStreamReader 子流读取器，作为父流读取器的子集。
type childStreamReader[T any] struct {
	parent *parentStreamReader[T] // 父流读取器
	index  int                    // 子流索引
}

// cpStreamElement 复制流链表中的元素节点，属于子流链表。
type cpStreamElement[T any] struct {
	once sync.Once           // 确保元素只被处理一次
	next *cpStreamElement[T] // 下一个元素节点
	item streamItem[T]       // 流数据项
}

// parentStreamReader 方法

// peek 获取指定索引的数据。
// 相同索引并发调用不安全，不同索引并发调用安全。
// 确保每个子流读取器在单个 goroutine 中使用 for 循环。
func (p *parentStreamReader[T]) peek(idx int) (t T, err error) {
	elem := p.subStreamList[idx]
	if elem == nil {
		// 子流关闭后意外调用接收
		return t, ErrRecvAfterClosed
	}

	// sync.Once 用于：
	// 1. 写入当前元素的数据内容
	// 2. 初始化当前元素的 next 字段为空元素（类似 copyStreamReaders 中的初始化）
	// 3. 将子流指针移动到下一个元素
	elem.once.Do(func() {
		t, err = p.sr.Recv()
		elem.item = streamItem[T]{chunk: t, err: err}
		if err != io.EOF {
			elem.next = &cpStreamElement[T]{}
			p.subStreamList[idx] = elem.next
		}
	})

	// 元素已设置且不会被再次修改
	// 因此子流可以并发读取该元素的内容和 next 指针
	t = elem.item.chunk
	err = elem.item.err
	if err != io.EOF {
		p.subStreamList[idx] = elem.next
	}

	return t, err
}

// close 关闭指定索引的子流。
// 所有子流关闭后，自动关闭原始流读取器。
func (p *parentStreamReader[T]) close(idx int) {
	// 重复关闭检查：避免重复关闭的安全性处理
	if p.subStreamList[idx] == nil {
		return // 避免重复关闭
	}
	p.subStreamList[idx] = nil

	// 原子计数：使用原子操作确保并发安全
	curClosedNum := atomic.AddUint32(&p.closedNum, 1)

	// 全量关闭：所有子流关闭时的清理逻辑
	allClosed := int(curClosedNum) == len(p.subStreamList)
	if allClosed {
		p.sr.Close()
	}
}

// childStreamReader 方法

// recv 从父流读取器中接收指定索引的数据。
func (csr *childStreamReader[T]) recv() (T, error) {
	return csr.parent.peek(csr.index)
}

// toStream 将子流读取器转换为流。
func (csr *childStreamReader[T]) toStream() *stream[T] {
	return toStream[T, *childStreamReader[T]](csr)
}

// close 关闭指定索引的子流。
func (csr *childStreamReader[T]) close() {
	csr.parent.close(csr.index)
}

// 4.4 内部工具函数

// copyStreamReaders 从单个流读取器创建多个独立的子流读取器。
// 每个子流读取器都可以独立地从原始流中读取数据。
func copyStreamReaders[T any](sr *StreamReader[T], n int) []*StreamReader[T] {
	cpsr := &parentStreamReader[T]{
		sr:            sr,
		subStreamList: make([]*cpStreamElement[T], n),
		closedNum:     0,
	}

	// 初始化子流列表，使用空元素作为尾节点
	// nil 元素表示子流已关闭，需要解引用
	// 原始通道长度未知时，链接前后元素具有挑战性
	// 使用前向指针会简化元素解引用，避免引用计数
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

// toStream 将读取器转换为流。
// 启动 goroutine 持续读取数据并发送到流中，处理 panic 并自动清理资源。
func toStream[T any, Reader reader[T]](r Reader) *stream[T] {
	ret := newStream[T](5)

	go func() {
		defer func() {
			// 捕获 panic 并转换为错误发送到流中
			panicErr := recover()
			if panicErr != nil {
				e := safe.NewPanicErr(panicErr, debug.Stack())

				var chunk T
				_ = ret.send(chunk, e)
			}

			// 清理资源：关闭发送端和读取器
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

// arrToStream 将数组转换为流。
// 一次性发送所有数组元素到流中，然后关闭发送端。
func arrToStream[T any](arr []T) *stream[T] {
	s := newStream[T](len(arr))
	for i := range arr {
		s.send(arr[i], nil)
	}
	s.closeSend()

	return s
}
