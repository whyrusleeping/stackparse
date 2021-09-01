package main

import (
	"encoding/json"
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
	helpstr := `
To filter out goroutines from the trace, use the following flags:
--frame-match=FOO or --fm=FOO
  print only stacks with frames that contain 'FOO'
--frame-not-match=FOO or --fnm=FOO
  print only stacks with no frames containing 'FOO'
--wait-more-than=10m
  print only stacks that have been blocked for more than ten minutes
--wait-less-than=10m
  print only stacks that have been blocked for less than ten minutes
--state-match=FOO
  print only stacks whose state matches 'FOO'
--state-not-match=FOO
  print only stacks whose state matches 'FOO'

Output is by default sorted by waittime ascending, to change this use:
--sort=[stacksize,goronum,waittime]

To print a summary of the goroutines in the stack trace, use:
--summary

If your stacks have some prefix to them (like a systemd log prefix) trim it with:
--line-prefix=prefixRegex

To print the output in JSON format, use:
--json or -j
`
	fmt.Println(helpstr)
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
	formatType := "default"
	fname := "-"

	var linePrefix string

	// parse flags
	for _, a := range os.Args[1:] {
		if strings.HasPrefix(a, "-") {
			parts := strings.Split(a, "=")
			var key string
			var val string
			key = parts[0]
			if len(parts) == 2 {
				val = parts[1]
			}

			switch key {
			case "--frame-match", "--fm":
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
			case "--frame-not-match", "--fnm":
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
					fmt.Println("valid options are: full, top, summary, json")
					os.Exit(1)
				}
			case "--summary", "-s":
				outputType = "summary"

			case "--json", "-j":
				formatType = "json"
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
		fmt.Println(err)
		os.Exit(1)
	}

	sorter := util.StackSorter{
		Stacks:   stacks,
		CompFunc: compfunc,
	}

	sort.Sort(sorter)

	var f formatter
	switch formatType {
	case "default":
		f = &defaultFormatter{}
	case "json":
		f = &jsonFormatter{}
	}

	// TODO: respect outputType
	_ = outputType

	stacks = util.ApplyFilters(stacks, filters)

	var (
		outputStr string
		formatErr error
	)

	switch outputType {
	case "full":
		outputStr, formatErr = f.formatStacks(stacks)
	case "summary":
		outputStr, formatErr = f.formatSummaries(summarize(stacks))
	default:
		fmt.Println("unrecognized output type: ", outputType)
		os.Exit(1)
	}
	if formatErr != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	fmt.Println(outputStr)

}

type summary struct {
	Function string
	Count    int
}

type formatter interface {
	formatSummaries([]summary) (string, error)
	formatStacks([]*util.Stack) (string, error)
}

type defaultFormatter struct{}

func (t *defaultFormatter) formatSummaries(summaries []summary) (string, error) {
	sb := &strings.Builder{}
	tw := tabwriter.NewWriter(sb, 8, 4, 2, ' ', 0)
	for _, s := range summaries {
		fmt.Fprintf(tw, "%s\t%d\n", s.Function, s.Count)
	}
	tw.Flush()
	return sb.String(), nil
}

func (t *defaultFormatter) formatStacks(stacks []*util.Stack) (string, error) {
	sb := &strings.Builder{}
	for _, s := range stacks {
		sb.WriteString(s.String())
		sb.WriteRune('\n')
	}
	return sb.String(), nil
}

type jsonFormatter struct{}

func (j *jsonFormatter) formatSummaries(summaries []summary) (string, error) {
	b, err := json.Marshal(summaries)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func (j *jsonFormatter) formatStacks(stacks []*util.Stack) (string, error) {
	b, err := json.Marshal(stacks)
	if err != nil {
		return "", err
	}
	return string(b), err
}

func summarize(stacks []*util.Stack) []summary {
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

	var summaries []summary
	for _, s := range filtered {
		summaries = append(summaries, summary{
			Function: s.Frames[0].Function,
			Count:    counts[s.Frames[0].Function],
		})
	}
	return summaries
}
