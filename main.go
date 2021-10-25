package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
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

	var repl bool

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

			case "--repl":
				repl = true
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
			case "--suspicious", "--sus":
				outputType = "sus"
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

	stacks = util.ApplyFilters(stacks, filters)

	var formatErr error

	switch outputType {
	case "full":
		formatErr = f.formatStacks(os.Stdout, stacks)
	case "summary":
		formatErr = f.formatSummaries(os.Stdout, summarize(stacks))
	case "sus":
		suspiciousCheck(util.ApplyFilters(stacks, filters))
	default:
		fmt.Println("unrecognized output type: ", outputType)
		os.Exit(1)
	}

	if formatErr != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	if repl {
		runRepl(util.ApplyFilters(stacks, filters))
	}
}

type summary struct {
	Function string
	Count    int
}

type formatter interface {
	formatSummaries(io.Writer, []summary) error
	formatStacks(io.Writer, []*util.Stack) error
}

type defaultFormatter struct{}

func (t *defaultFormatter) formatSummaries(w io.Writer, summaries []summary) error {
	tw := tabwriter.NewWriter(w, 8, 4, 2, ' ', 0)
	for _, s := range summaries {
		fmt.Fprintf(tw, "%s\t%d\n", s.Function, s.Count)
	}
	tw.Flush()
	return nil
}

func (t *defaultFormatter) formatStacks(w io.Writer, stacks []*util.Stack) error {
	for _, s := range stacks {
		fmt.Fprintln(w, s.String())
	}
	return nil
}

type jsonFormatter struct{}

func (j *jsonFormatter) formatSummaries(w io.Writer, summaries []summary) error {
	return json.NewEncoder(w).Encode(summaries)
}

func (j *jsonFormatter) formatStacks(w io.Writer, stacks []*util.Stack) error {
	return json.NewEncoder(w).Encode(stacks)
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

type framecount struct {
	frameKey string
	count    int
}

// work in progress, trying to come up with an algorithm to point out suspicious stacks
func suspiciousCheck(stacks []*util.Stack) {
	sharedFrames := make(map[string][]*util.Stack)

	for _, s := range stacks {
		for _, f := range s.Frames {
			fk := f.FrameKey()
			sharedFrames[fk] = append(sharedFrames[fk], s)
		}
	}

	var fcs []framecount
	for k, v := range sharedFrames {
		fcs = append(fcs, framecount{
			frameKey: k,
			count:    len(v),
		})
	}

	sort.Slice(fcs, func(i, j int) bool {
		return fcs[i].count > fcs[j].count
	})

	for i := 0; i < 20; i++ {
		fmt.Printf("%s - %d\n", fcs[i].frameKey, fcs[i].count)
	}

	for i := 0; i < 5; i++ {
		fmt.Println("-------- FRAME SUS STAT ------")
		fmt.Printf("%s - %d\n", fcs[i].frameKey, fcs[i].count)

		sf := sharedFrames[fcs[i].frameKey]

		printUnique(sf)
	}
}

func printUnique(stacks []*util.Stack) {
	var ftypes []*util.Stack
	var bucketed [][]*util.Stack
	for _, s := range stacks {
		var found bool
		for x, ft := range ftypes {
			if s.Sameish(ft) {
				bucketed[x] = append(bucketed[x], s)
				found = true
			}
		}

		if !found {
			ftypes = append(ftypes, s)
			bucketed = append(bucketed, []*util.Stack{s})
		}
	}

	for x, ft := range ftypes {
		fmt.Println("count: ", len(bucketed[x]))
		fmt.Println("average wait: ", compWaitStats(bucketed[x]).String())
		fmt.Println(ft.String())
		fmt.Println()
	}
}

type waitStats struct {
	Average time.Duration
	Max     time.Duration
	Min     time.Duration
	Median  time.Duration
}

func (ws waitStats) String() string {
	return fmt.Sprintf("av/min/max/med: %s/%s/%s/%s\n", ws.Average, ws.Min, ws.Max, ws.Median)
}

func compWaitStats(stacks []*util.Stack) waitStats {
	var durations []time.Duration
	var min, max, sum time.Duration
	for _, s := range stacks {
		if min == 0 || s.WaitTime < min {
			min = s.WaitTime
		}
		if s.WaitTime > max {
			max = s.WaitTime
		}

		sum += s.WaitTime
		durations = append(durations, s.WaitTime)
	}

	sort.Slice(durations, func(i, j int) bool {
		return durations[i] < durations[j]
	})

	return waitStats{
		Average: sum / time.Duration(len(durations)),
		Max:     max,
		Min:     min,
		Median:  durations[len(durations)/2],
	}
}

func frameStat(stacks []*util.Stack) {
	frames := make(map[string]int)

	for _, s := range stacks {
		for _, f := range s.Frames {
			frames[fmt.Sprintf("%s:%d\n%s", f.File, f.Line, f.Function)]++
		}
	}

	type frameCount struct {
		Line  string
		Count int
	}

	var fcs []frameCount
	for k, v := range frames {
		fcs = append(fcs, frameCount{
			Line:  k,
			Count: v,
		})
	}

	sort.Slice(fcs, func(i, j int) bool {
		return fcs[i].Count < fcs[j].Count
	})

	for _, fc := range fcs {
		fmt.Printf("%s\t%d\n", fc.Line, fc.Count)
	}
}

func runRepl(input []*util.Stack) {
	bynumber := make(map[int]*util.Stack)
	for _, i := range input {
		bynumber[i.Number] = i
	}

	stk := [][]*util.Stack{input}
	ops := []string{"."}

	cur := input

	f := &defaultFormatter{}

	scan := bufio.NewScanner(os.Stdin)
	fmt.Print("stackparse> ")
	for scan.Scan() {
		parts := strings.Split(scan.Text(), " ")
		switch parts[0] {
		case "fm", "frame-match":

			var filters []util.Filter
			for _, p := range parts[1:] {
				filters = append(filters, util.HasFrameMatching(strings.TrimSpace(p)))
			}

			cur = util.ApplyFilters(cur, filters)
			stk = append(stk, cur)
			ops = append(ops, scan.Text())

		case "fnm", "frame-not-match":
			var filters []util.Filter
			for _, p := range parts[1:] {
				filters = append(filters, util.Negate(util.HasFrameMatching(strings.TrimSpace(p))))
			}

			cur = util.ApplyFilters(cur, filters)
			stk = append(stk, cur)
			ops = append(ops, scan.Text())
		case "s", "summary", "sum":
			err := f.formatSummaries(os.Stdout, summarize(cur))
			if err != nil {
				fmt.Println(err)
			}
		case "show", "p", "print":
			if len(parts) > 1 {
				num, err := strconv.Atoi(parts[1])
				if err != nil {
					fmt.Println(err)
					goto end
				}
				s, ok := bynumber[num]
				if !ok {
					fmt.Println("no stack found with that number")
					goto end
				}
				f.formatStacks(os.Stdout, []*util.Stack{s})
			} else {
				f.formatStacks(os.Stdout, cur)
			}
		case "diff":
			for i, op := range ops {
				fmt.Printf("%d (%d): %s\n", i, len(stk[i]), op)
			}
		case "pop":
			if len(stk) > 1 {
				stk = stk[:len(stk)-1]
				ops = ops[:len(ops)-1]

				cur = stk[len(stk)-1]
			}
		case "sus":
			// WIP!!!
			suspiciousCheck(cur)
		case "framestat":
			frameStat(cur)
		case "unique", "uu":
			printUnique(cur)
		}

	end:
		fmt.Print("stackparse> ")
	}
}
