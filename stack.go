package pdf2txt

type stack struct {
	items []interface{}
}

func (s *stack) Len() int {
	return len(s.items)
}

func (s *stack) Push(value interface{}) {
	s.items = append(s.items, value)
}

func (s *stack) LPop() (value interface{}) {
	l := len(s.items)
	if l == 0 {
		return nil
	}
	v := s.items[0]
	s.items = s.items[1:]
	return v
}

func (s *stack) Pop() (value interface{}) {
	l := len(s.items)
	if l == 0 {
		return nil
	}
	v := s.items[l-1]
	s.items = s.items[:l-1]
	return v
}
