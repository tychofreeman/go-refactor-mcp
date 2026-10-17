package m

import "fmt"

func cons(xs []int, ok bool) []int {
    if ok {
        output := []int{0}
        for _, i := range xs {
            output = append(output, i)
        }
        return output
    }
    fmt.Println("no")
    return nil
}
