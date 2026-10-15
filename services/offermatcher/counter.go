package main

type Counter struct {
	Value int
}

func (c *Counter) IncrementCounter() {
	c.Value++
}
