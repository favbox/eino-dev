package schema

const maxSelectNum = 5

// receiveN 从多个流中接收数据，使用函数表优化小数据流的性能。
// 根据流数量选择对应的接收策略：直接接收或使用 select 语句
func receiveN[T any](chosenList []int, ss []*stream[T]) (int, *streamItem[T], bool) {
	return []func(chosenList []int, ss []*stream[T]) (index int, item *streamItem[T], ok bool){
		nil, // 0 个流：无操作
		func(chosenList []int, ss []*stream[T]) (int, *streamItem[T], bool) {
			// 1 个流：直接接收
			item, ok := <-ss[chosenList[0]].items
			return chosenList[0], &item, ok
		},
		func(chosenList []int, ss []*stream[T]) (int, *streamItem[T], bool) {
			// 2 个流：使用 select 选择
			select {
			case item, ok := <-ss[chosenList[0]].items:
				return chosenList[0], &item, ok
			case item, ok := <-ss[chosenList[1]].items:
				return chosenList[1], &item, ok
			}
		},
		func(chosenList []int, ss []*stream[T]) (int, *streamItem[T], bool) {
			// 3 个流：使用 select 选择
			select {
			case item, ok := <-ss[chosenList[0]].items:
				return chosenList[0], &item, ok
			case item, ok := <-ss[chosenList[1]].items:
				return chosenList[1], &item, ok
			case item, ok := <-ss[chosenList[2]].items:
				return chosenList[2], &item, ok
			}
		},
		func(chosenList []int, ss []*stream[T]) (int, *streamItem[T], bool) {
			// 4 个流：使用 select 选择
			select {
			case item, ok := <-ss[chosenList[0]].items:
				return chosenList[0], &item, ok
			case item, ok := <-ss[chosenList[1]].items:
				return chosenList[1], &item, ok
			case item, ok := <-ss[chosenList[2]].items:
				return chosenList[2], &item, ok
			case item, ok := <-ss[chosenList[3]].items:
				return chosenList[3], &item, ok
			}
		},
		func(chosenList []int, ss []*stream[T]) (int, *streamItem[T], bool) {
			// 5 个流：使用 select 选择
			select {
			case item, ok := <-ss[chosenList[0]].items:
				return chosenList[0], &item, ok
			case item, ok := <-ss[chosenList[1]].items:
				return chosenList[1], &item, ok
			case item, ok := <-ss[chosenList[2]].items:
				return chosenList[2], &item, ok
			case item, ok := <-ss[chosenList[3]].items:
				return chosenList[3], &item, ok
			case item, ok := <-ss[chosenList[4]].items:
				return chosenList[4], &item, ok
			}
		},
	}[len(chosenList)](chosenList, ss)
}
