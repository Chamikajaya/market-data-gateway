### select statement

* `select` is like a switch statement for channels. When go runtime hits a `select` block it pauses and waits. It monitors all the channels listed in the `case` statement at once. As soon as one of those channels is ready to send or receive data, `select` executes that specific case and then finishes.

* The limitation of `select` is that it only evaluates once. Once it handles a single channel message, it's done. For example if we are building a system that needs to continuously process incoming ws events / needs to continously listen for a shutdown signal, a single `select` is not enough. So in such cases we need to wrap the `select` in an infinite `for` loop.

``` go
for {
    select {
    case msg := <-wsBuffer:
        // Process standard data
        fmt.Println("Received:", msg)
    case <-ctx.Done():
        // The context was canceled! Clean up and exit the loop.
        fmt.Println("Shutting down worker...")
        return 
    }
}
```

**non-blocking channel operations**

* Sometimes we want to try to send or receive data on a channel, but if the channel is full (or empty), we want the program to immediately move on and do something else rather than freezing. We do this by adding a `default` case.

```go
for {
    select {
    case msg := <-highPriorityQueue:
        process(msg)
    default:
        // If the highPriorityQueue is empty, don't wait!
        // Immediately do background work instead.
        doBackgroundCleanup()
        time.Sleep(100 * time.Millisecond)
    }
}
```

### Named return values

* Instead of just stating the types that a function will return, you give those return values names. Go treats these named returns as variables defined at the very top of our function.

```go




// Standard return: We only specify the types (int, int)
func calculateStandard(length, width int) (int, int) {
	area := length * width
	perimeter := 2 * (length + width)
	return area, perimeter // Must specify what is being returned
}

// Named return: We name the variables (area, perimeter int)
func calculateNamed(length, width int) (area, perimeter int) {
	// Notice we use '=' instead of ':=' because the variables 
	// are already declared in the function signature.
	area = length * width
	perimeter = 2 * (length + width)
	
	return // "Naked" return automatically returns area and perimeter
}

func main() {
	a, p := calculateNamed(5, 4)
	fmt.Printf("Area: %d, Perimeter: %d\n", a, p)
}

```

**modifying returns in `defer` statements**

* Because named returns are standard variables, defer functions can access and modify them after the return statement has been executed, but before the function actually hands control back to the caller.

```go
func doSomething() (err error) {
	defer func() {
		if r := recover(); r != nil {
			// We can capture a panic and assign it to the named return 'err'
			err = fmt.Errorf("function panicked: %v", r)
		}
	}()

	panic("oops!")
	return nil // This will be overwritten by the defer block
}
```


### wait-groups