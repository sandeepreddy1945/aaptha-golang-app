package main

// package main

// import (
// 	"fmt"

// 	"rsc.io/quote"
// )

// func main() {
// 	fmt.Println("Hello, World!")
// 	card := newCard()

// 	fmt.Println(card)

// 	fmt.Println(generateQuote())

// 	a, b := 1, 2

// 	fmt.Println(a, b)

// 	forLoop(5)
// }

// func newCard() string {
// 	return "Ace of Spades"
// }

// func generateQuote() string {
// 	return quote.Go()
// }

// func forLoop(n int) {
// 	sum := 0
// 	for v := range n {
// 		sum += v
// 	}

// 	fmt.Println("Sum of Numbers is: ", sum)
// }

// package main

// import "fmt"

// type rect struct {
// 	width, height int
// }

// func (r *rect) area() int {
// 	return r.width * r.height
// }

// func (r rect) perim() int {
// 	return 2*r.width + 2*r.height
// }

// func main() {
// 	r := rect{width: 10, height: 5}

// 	fmt.Println("area: ", r.area())
// 	fmt.Println("perim:", r.perim())

// 	rp := &r
// 	fmt.Println("area: ", rp.area())
// 	fmt.Println("perim:", rp.perim())
// }

// package main

// import (
// 	"fmt"
// 	"time"
// )

// func f(from string) {
// 	for i := range 3 {
// 		fmt.Println(from, ":", i)
// 	}
// }

// func main() {

// 	f("direct")

// 	go f("goroutine")

// 	go func(msg string) {
// 		fmt.Println(msg)
// 	}("going")

// 	time.Sleep(time.Second)
// 	fmt.Println("done")
// }

// package main

// import "fmt"

// func main() {

// 	messages := make(chan string)

// 	go func() { messages <- "ping" }()

// 	msg := <-messages
// 	fmt.Println(msg)
// }

// package main

// import (
// 	"fmt"
// 	"sync"
// 	"time"
// )

// func worker(id int) {
// 	fmt.Printf("Worker %d starting\n", id)

// 	time.Sleep(time.Second)
// 	fmt.Printf("Worker %d done\n", id)
// }

// func main() {

// 	var wg sync.WaitGroup

// 	for i := 1; i <= 5; i++ {
// 		wg.Go(func() {
// 			worker(i)
// 		})
// 	}

// 	wg.Wait()

// }

// package main

// import (
// 	"fmt"
// 	"time"
// )

// func main() {

// 	requests := make(chan int, 5)
// 	for i := 1; i <= 5; i++ {
// 		requests <- i
// 	}
// 	close(requests)

// 	limiter := time.Tick(200 * time.Millisecond)

// 	for req := range requests {
// 		<-limiter
// 		fmt.Println("request", req, time.Now())
// 	}

// 	burstyLimiter := make(chan time.Time, 3)

// 	for range 3 {
// 		burstyLimiter <- time.Now()
// 	}

// 	go func() {
// 		for t := range time.Tick(200 * time.Millisecond) {
// 			burstyLimiter <- t
// 		}
// 	}()

// 	burstyRequests := make(chan int, 5)
// 	for i := 1; i <= 5; i++ {
// 		burstyRequests <- i
// 	}
// 	close(burstyRequests)
// 	for req := range burstyRequests {
// 		<-burstyLimiter
// 		fmt.Println("request", req, time.Now())
// 	}
// }

// package main

// import (
// 	"fmt"
// 	"os"
// )

// type point struct {
// 	x, y int
// }

// func main() {

// 	p := point{1, 2}
// 	fmt.Printf("struct1: %v\n", p)

// 	fmt.Printf("struct2: %+v\n", p)

// 	fmt.Printf("struct3: %#v\n", p)

// 	fmt.Printf("type: %T\n", p)

// 	fmt.Printf("bool: %t\n", true)

// 	fmt.Printf("int: %d\n", 123)

// 	fmt.Printf("bin: %b\n", 14)

// 	fmt.Printf("char: %c\n", 33)

// 	fmt.Printf("hex: %x\n", 456)

// 	fmt.Printf("float1: %f\n", 78.9)

// 	fmt.Printf("float2: %e\n", 123400000.0)
// 	fmt.Printf("float3: %E\n", 123400000.0)

// 	fmt.Printf("str1: %s\n", "\"string\"")

// 	fmt.Printf("str2: %q\n", "\"string\"")

// 	fmt.Printf("str3: %x\n", "hex this")

// 	fmt.Printf("pointer: %p\n", &p)

// 	fmt.Printf("width1: |%6d|%6d|\n", 12, 345)

// 	fmt.Printf("width2: |%6.2f|%6.2f|\n", 1.2, 3.45)

// 	fmt.Printf("width3: |%-6.2f|%-6.2f|\n", 1.2, 3.45)

// 	fmt.Printf("width4: |%6s|%6s|\n", "foo", "b")

// 	fmt.Printf("width5: |%-6s|%-6s|\n", "foo", "b")

// 	s := fmt.Sprintf("sprintf: a %s", "string")
// 	fmt.Println(s)

// 	fmt.Fprintf(os.Stderr, "io: an %s\n", "error")
// }
