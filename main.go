package main

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	util "github.com/whyrusleeping/stackparse/util"
)

func printHelp() {
	fmt.Println("to filter out goroutines from the trace, use the following flags:")
	fmt.Println("--frame-match=FOO")
	fmt.Println("  print only stacks with frames that contain 'FOO'")
	fmt.Println("--frame-not-match=FOO")
	fmt.Println("  print only stacks with no frames containing 'FOO'")
	fmt.Println("--wait-more-than=10m")
	fmt.Println("  print only stacks that have been blocked for more than ten minutes")
	fmt.Println("--wait-less-than=10m")
	fmt.Println("  print only stacks that have been blocked for less than ten minutes")
	fmt.Println("\n")
	fmt.Println("output is by default sorted by waittime ascending, to change this use:")
	fmt.Println("--sort=[stacksize,goronum,waittime]")
}

func main() {
	if len(os.Args) < 2 || os.Args[1] == "-h" || os.Args[1] == "--help" {
		fmt.Printf("usage: %s <filter flags> <filename>\n", os.Args[0])
		printHelp()
		return
	}

	var filters []util.Filter
	var compfunc util.StackCompFunc = util.CompWaitTime
	outputType := "full"
	fname := "-"

	var linePrefix string

	// parse flags
	for _, a := range os.Args[1:] {
		if strings.HasPrefix(a, "--") {
			parts := strings.Split(a, "=")
			var key string
			var val string
			key = parts[0]
			if len(parts) == 2 {
				val = parts[1]
			}

			switch key {
			case "--frame-match":
				filters = append(filters, util.HasFrameMatching(val))
			case "--wait-more-than":
				d, err := time.ParseDuration(val)
				if err != nil {
					fmt.Println(err)
					os.Exit(1)
				}
				filters = append(filters, util.TimeGreaterThan(d))
			case "--wait-less-than":
				d, err := time.ParseDuration(val)
				if err != nil {
					fmt.Println(err)
					os.Exit(1)
				}
				filters = append(filters, util.Negate(util.TimeGreaterThan(d)))
			case "--frame-not-match":
				filters = append(filters, util.Negate(util.HasFrameMatching(val)))
			case "--state-match":
				filters = append(filters, util.MatchState(val))
			case "--state-not-match":
				filters = append(filters, util.Negate(util.MatchState(val)))
			case "--sort":
				switch parts[1] {
				case "goronum":
					compfunc = util.CompGoroNum
				case "stacksize":
					compfunc = util.CompDepth
				case "waittime":
					compfunc = util.CompWaitTime
				default:
					fmt.Println("unknown sorting parameter: ", val)
					fmt.Println("options: goronum, stacksize, waittime (default)")
					os.Exit(1)
				}
			case "--line-prefix":
				linePrefix = val

			case "--output":
				switch val {
				case "full", "top", "summary":
					outputType = val
				default:
					fmt.Println("unrecognized output type: ", parts[1])
					fmt.Println("valid options are: full, top")
					os.Exit(1)
				}
			case "--summary":
				outputType = "summary"
			}
		} else {
			fname = a
		}
	}

	var r io.Reader
	if fname == "-" {
		r = os.Stdin
	} else {
		fi, err := os.Open(fname)
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		defer fi.Close()

		r = fi
	}

	stacks, err := util.ParseStacks(r, linePrefix)
	if err != nil {
		panic(err)
	}

	sorter := util.StackSorter{
		Stacks:   stacks,
		CompFunc: compfunc,
	}

	sort.Sort(sorter)

	// TODO: respect outputType
	_ = outputType

	switch outputType {
	case "full":
		for _, s := range util.ApplyFilters(stacks, filters) {
			s.Print()
		}
	case "summary":
		printSummary(util.ApplyFilters(stacks, filters))
	default:
		fmt.Println("unrecognized output type: ", outputType)
		os.Exit(1)
	}
}

func printSummary(stacks []*util.Stack) {
	counts := make(map[string]int)

	var filtered []*util.Stack

	for _, s := range stacks {
		f := s.Frames[0].Function
		if counts[f] == 0 {
			filtered = append(filtered, s)
		}
		counts[f]++
	}

	sort.Sort(util.StackSorter{
		Stacks: filtered,
		CompFunc: func(a, b *util.Stack) bool {
			return counts[a.Frames[0].Function] < counts[b.Frames[0].Function]
		},
	})

	tw := tabwriter.NewWriter(os.Stdout, 8, 4, 2, ' ', 0)
	for _, s := range filtered {
		f := s.Frames[0].Function
		fmt.Fprintf(tw, "%s\t%d\n", f, counts[f])
	}
	tw.Flush()
}
