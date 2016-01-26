package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

type Stack struct {
	Number   int
	State    string
	WaitTime string
	Frames   []Frame
}

func (s *Stack) Print() {
	state := s.State
	if s.WaitTime != "" {
		state += ", " + s.WaitTime
	}
	fmt.Printf("goroutine %d [%s]:\n", s.Number, s.WaitTime)
	for _, f := range s.Frames {
		f.Print()
	}

	fmt.Println()
}

type Frame struct {
	Function string
	File     string
	Line     int
}

func (f *Frame) Print() {
	fmt.Println(f.Function)
	fmt.Printf("\t%s:%d\n", f.File, f.Line)
}

type Filter func(s *Stack) bool

func HasFrameMatching(pattern string) Filter {
	return func(s *Stack) bool {
		for _, f := range s.Frames {
			if strings.Contains(f.Function, pattern) || strings.Contains(f.File, pattern) {
				return true
			}
		}
		return false
	}
}

func Negate(f Filter) Filter {
	return func(s *Stack) bool {
		return !f(s)
	}
}

func ApplyFilters(stacks []*Stack, filters []Filter) []*Stack {
	var out []*Stack

next:
	for _, s := range stacks {
		for _, f := range filters {
			if !f(s) {
				continue next
			}
		}
		out = append(out, s)
	}
	return out
}

func main() {
	if len(os.Args) < 2 {
		fmt.Printf("usage: %s <filename>\n", os.Args[0])
		return
	}

	var r io.Reader
	fname := os.Args[1]
	if fname == "-" {
		r = os.Stdin
	} else {
		fi, err := os.Open(fname)
		if err != nil {
			panic(err)
		}
		defer fi.Close()

		r = fi
	}

	stacks, err := ParseStacks(r)
	if err != nil {
		panic(err)
	}

	filters := []Filter{
		HasFrameMatching("mfs"),
	}

	for _, s := range ApplyFilters(stacks, filters) {
		s.Print()
	}
}

func ParseStacks(r io.Reader) ([]*Stack, error) {

	var cur *Stack
	var stacks []*Stack
	var frame *Frame
	scan := bufio.NewScanner(r)
	for scan.Scan() {
		if strings.HasPrefix(scan.Text(), "goroutine") {
			parts := strings.Split(scan.Text(), " ")
			num, err := strconv.Atoi(parts[1])
			if err != nil {
				return nil, fmt.Errorf("unexpected formatting: %s", scan.Text())
			}

			var time string
			state := strings.Split(strings.Trim(strings.Join(parts[2:], " "), "[]:"), ",")
			if len(state) > 1 {
				time = state[1]
			}

			cur = &Stack{
				Number:   num,
				State:    state[0],
				WaitTime: time,
			}
			continue
		}
		if scan.Text() == "" {
			stacks = append(stacks, cur)
			cur = nil
			continue
		}

		if frame == nil {
			frame = &Frame{
				Function: scan.Text(),
			}
		} else {
			parts := strings.Split(scan.Text(), ":")
			frame.File = strings.Trim(parts[0], " \t\n")

			lnum, err := strconv.Atoi(strings.Split(parts[1], " ")[0])
			if err != nil {
				return nil, fmt.Errorf("error finding line number: ", scan.Text())
			}

			frame.Line = lnum
			cur.Frames = append(cur.Frames, *frame)
			frame = nil
		}
	}

	if cur != nil {
		stacks = append(stacks, cur)
	}

	return stacks, nil
}
