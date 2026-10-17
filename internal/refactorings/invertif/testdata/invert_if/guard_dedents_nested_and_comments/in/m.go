package m

import "fmt"

func run(items []string, verbose bool) {
	if verbose { // only when asked
		// Print each item.
		for _, it := range items {
			if it != "" {
				fmt.Println(it)
			}
		}

		fmt.Println(`done
  (raw string, unchanged)`)
	}
}
