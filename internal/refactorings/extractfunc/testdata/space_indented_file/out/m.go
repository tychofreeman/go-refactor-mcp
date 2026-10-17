package m

import "fmt"

func cons(xs []int, ok bool) []int {
    if ok {
        return prepend(xs)
    }
    fmt.Println("no")
    return nil
}

func prepend(xs []int) []int {
    output := []int{0}
    for _, i := range xs {
        output = append(output, i)
    }
    return output
}
