package main

import (
	"context"
	"fmt"
)

type User struct {
	ID       int64
	Email    string
	Username string
	Admin    bool
}

func increment(value *int64) {
	*value++
}

func NewUser(email string) User {
	return User{
		Email: email,
		Admin: false,
	}
}

func divide(a, b float64) (float64, error) {
	if b == 0 {
		return 0, fmt.Errorf("division by zero")
	}

	return a / b, nil
}

func doSomethingOnStorage(storage Storage) {
	ctx := context.Background()
	if storage != nil {
		storage.Save(ctx, User{})
	}
}

func First[T any](items []T) (T, bool) {
	if len(items) == 0 {
		var zero T
		return zero, false
	}

	return items[0], true
}

func main() {
	// name := "Sebastian"
	// years := 40

	// fmt.Printf("%s has %d years of experience\n", name, years)

	// fmt.Println("Concurrent worker pool:")
	// DoSomething()

	// user := User{
	// 	ID:       1,
	// 	Email:    name + "@gmail.com",
	// 	Admin:    true,
	// 	Username: "test",
	// }
	// fmt.Println(user)

	// storage := PostgresStorage{}
	// doSomethingOnStorage(&storage)

	// n := int64(10)
	// increment(&n)
	// fmt.Println(n) // 11

	// result, err := divide(float64(n), 2)
	// if err != nil {
	// 	panic(err)
	// }

	// fmt.Println(result)

	// cntr := Counter{Value: 1}
	// cntr.IncrementCounter()

	// fmt.Println(cntr.Value)

	// fmt.Println("-----------------------------------------------")

	// numbers := []int{10, 20, 30}
	// numbers = append(numbers, 40)
	// fmt.Println(len(numbers))
	// fmt.Println(cap(numbers))

	// fmt.Println("-----------------------------------------------")
	// numbers = append(numbers, 50)
	// fmt.Println(len(numbers))
	// fmt.Println(cap(numbers))

	// fmt.Println("-----------------------------------------------")
	// numbers = append(numbers, 60)
	// fmt.Println(len(numbers))
	// fmt.Println(cap(numbers))

	// fmt.Println("-----------------------------------------------")
	// numbers = append(numbers, 70)
	// fmt.Println(len(numbers))
	// fmt.Println(cap(numbers))

	// // first of numbers
	// nomberro, exists := First(numbers)
	// fmt.Println("First of numbers is: ", nomberro)

	// fmt.Println("-----------------------------------------------")

	// scores := map[string]int{
	// 	"backend": 90,
	// 	"go":      70,
	// }

	// scores["architecture"] = 95
	// score, exists := scores["rust"]
	// if !exists {
	// 	// no worries, do nothing
	// }
	// fmt.Println(score) // this prints "0" then

	// scoreForBackend, exists := scores["backend"]
	// if !exists {
	// 	// same, no worries, but it does exist
	// }
	// fmt.Println("Backend score is ", scoreForBackend)

	// Now concurrency
	// DoConcurrentWorkForLearning()

	ConcurrentRequests()
}
