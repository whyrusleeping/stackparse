package stacks

import (
	"bufio"
	"fmt"
	"io"
	"regexp"
	"runtime/debug"
	"strconv"
	"strings"
	"time"
)

type Stack struct {
	Number       int
	State        string
	WaitTime     time.Duration
	Frames       []Frame
	ThreadLocked bool
	CreatedBy    CreatedBy
}

func (s *Stack) String() string {
	sb := strings.Builder{}
	state := s.State
	waitTime := int(s.WaitTime.Minutes())
	if waitTime != 0 {
		state += ", " + fmt.Sprintf("%d minutes", waitTime)
	}
	sb.WriteString(fmt.Sprintf("goroutine %d [%s]:\n", s.Number, state))
	for _, f := range s.Frames {
		sb.WriteString(f.String())
		sb.WriteRune('\n')
	}
	sb.WriteString(s.CreatedBy.String())
	sb.WriteRune('\n')
	return sb.String()
}

type Frame struct {
	Function string
	Params   []string
	File     string
	Line     int64
	Entry    int64
}

func (f *Frame) String() string {
	sb := strings.Builder{}
	sb.WriteString(fmt.Sprintf("%s(%s)\n", f.Function, strings.Join(f.Params, ", ")))
	sb.WriteString(fmt.Sprintf("\t%s:%d", f.File, f.Line))
	if f.Entry != 0 {
		sb.WriteString(fmt.Sprintf(" %+#x", f.Entry))
	}
	return sb.String()
}

type CreatedBy struct {
	Function string
	File     string
	Line     int64
	Entry    int64
}

func (c *CreatedBy) String() string {
	sb := strings.Builder{}
	sb.WriteString(fmt.Sprintf("created by %s\n", c.Function))
	sb.WriteString(fmt.Sprintf("\t%s:%d", c.File, c.Line))
	if c.Entry != 0 {
		sb.WriteString(fmt.Sprintf(" %+#x", c.Entry))
	}
	return sb.String()
}

type Filter func(s *Stack) bool

func HasFrameMatching(pattern string) Filter {
	return func(s *Stack) bool {
		for _, f := range s.Frames {
			if strings.Contains(f.Function, pattern) || strings.Contains(fmt.Sprintf("%s:%d", f.File, f.Line), pattern) {
				return true
			}
		}
		return false
	}
}

func MatchState(st string) Filter {
	return func(s *Stack) bool {
		return s.State == st
	}
}

func TimeGreaterThan(d time.Duration) Filter {
	return func(s *Stack) bool {
		return s.WaitTime >= d
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

func ParseStacks(r io.Reader, linePrefix string) (_ []*Stack, _err error) {
	var re *regexp.Regexp

	if linePrefix != "" {
		r, err := regexp.Compile(linePrefix)
		if err != nil {
			return nil, fmt.Errorf("failed to compile line prefix regexp")
		}
		re = r
	}

	// Catch parsing errors and recover. There's no reason to crash the entire parser.
	// Also report the line number where the error happened.
	lineNo := 0
	defer func() {
		if r := recover(); r != nil {
			_err = fmt.Errorf("line %d: [panic] %s\n%s", lineNo, r, debug.Stack())
		} else if _err != nil {
			_err = fmt.Errorf("line %d: %w", lineNo, _err)
		}
	}()

	var cur *Stack
	var stacks []*Stack
	var frame *Frame
	scan := bufio.NewScanner(r)
	for scan.Scan() {
		lineNo++
		line := scan.Text()
		if re != nil {
			pref := re.Find([]byte(line))
			if len(pref) == len(line) {
				line = ""
			} else {
				line = line[len(pref):]
				line = strings.TrimSpace(line)
			}
		}

		if strings.HasPrefix(line, "goroutine") {
			if cur != nil {
				stacks = append(stacks, cur)
				cur = nil
			}

			parts := strings.Split(line, " ")
			num, err := strconv.Atoi(parts[1])
			if err != nil {
				return nil, fmt.Errorf("unexpected formatting: %s", line)
			}

			var timev time.Duration
			state := strings.Split(strings.Trim(strings.Join(parts[2:], " "), "[]:"), ",")
			locked := false
			// The first field is always the state. The second and
			// third are the time and whether or not it's locked to
			// the current thread. However, either or both of these fields can be omitted.
			for _, s := range state[1:] {
				if s == " locked to thread" {
					locked = true
					continue
				}
				timeparts := strings.Fields(state[1])
				if len(timeparts) != 2 {
					return nil, fmt.Errorf("weirdly formatted time string: %q", state[1])
				}

				val, err := strconv.Atoi(timeparts[0])
				if err != nil {
					return nil, err
				}

				timev = time.Duration(val) * time.Minute
			}

			cur = &Stack{
				Number:       num,
				State:        state[0],
				WaitTime:     timev,
				ThreadLocked: locked,
			}
			continue
		}
		if line == "" {
			// This can happen when we get random empty lines.
			if cur != nil {
				stacks = append(stacks, cur)
			}
			cur = nil
			continue
		}

		if strings.HasPrefix(line, "created by") {
			fn := strings.TrimPrefix(line, "created by ")
			if !scan.Scan() {
				return nil, fmt.Errorf("no file info after 'created by' line on line %d", lineNo)
			}
			file, line, entry, err := parseEntryLine(scan.Text())
			if err != nil {
				return nil, err
			}
			cur.CreatedBy = CreatedBy{
				Function: fn,
				File:     file,
				Line:     line,
				Entry:    entry,
			}
		} else if frame == nil {
			frame = &Frame{
				Function: line,
			}

			n := strings.LastIndexByte(line, '(')
			if n > -1 {
				frame.Function = line[:n]
				frame.Params = strings.Split(line[n+1:len(line)-1], ", ")
			}

		} else {
			file, line, entry, err := parseEntryLine(line)
			if err != nil {
				return nil, err
			}
			frame.File = file
			frame.Line = line
			frame.Entry = entry
			cur.Frames = append(cur.Frames, *frame)
			frame = nil
		}
	}
	if cur != nil {
		stacks = append(stacks, cur)
	}

	return stacks, nil
}

func parseEntryLine(s string) (file string, line int64, entry int64, err error) {
	parts := strings.Split(s, ":")
	file = strings.Trim(parts[0], " \t\n")
	if len(parts) != 2 {
		return "", 0, 0, fmt.Errorf("expected a colon: %q", line)
	}

	lineAndEntry := strings.Split(parts[1], " ")
	line, err = strconv.ParseInt(lineAndEntry[0], 0, 64)
	if err != nil {
		return "", 0, 0, fmt.Errorf("error parsing line number: %s", lineAndEntry[0])
	}
	if len(lineAndEntry) > 1 {
		entry, err = strconv.ParseInt(lineAndEntry[1], 0, 64)
		if err != nil {
			return "", 0, 0, fmt.Errorf("error parsing entry offset: %s", lineAndEntry[1])
		}
	}
	return
}

type StackCompFunc func(a, b *Stack) bool
type StackSorter struct {
	Stacks   []*Stack
	CompFunc StackCompFunc
}

func (ss StackSorter) Len() int {
	return len(ss.Stacks)
}

func (ss StackSorter) Less(i, j int) bool {
	return ss.CompFunc(ss.Stacks[i], ss.Stacks[j])
}

func (ss StackSorter) Swap(i, j int) {
	ss.Stacks[i], ss.Stacks[j] = ss.Stacks[j], ss.Stacks[i]
}

func CompWaitTime(a, b *Stack) bool {
	return a.WaitTime < b.WaitTime
}

func CompDepth(a, b *Stack) bool {
	return len(a.Frames) < len(b.Frames)
}

func CompGoroNum(a, b *Stack) bool {
	return a.Number < b.Number
}
