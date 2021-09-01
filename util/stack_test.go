package stacks

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestParseStacks(t *testing.T) {
	cases := []struct {
		name       string
		input      string
		linePrefix string

		expected []*Stack
	}{
		{
			name: "single goroutine",
			input: `
goroutine 85751948 [semacquire, 25 minutes]:
sync.runtime_Semacquire(0xc099422a74)
	/usr/local/go/src/runtime/sema.go:56 +0x45
sync.(*WaitGroup).Wait(0xc099422a74)
	/usr/local/go/src/sync/waitgroup.go:130 +0x65
github.com/libp2p/go-libp2p-swarm.(*Swarm).notifyAll(0xc000783380, 0xc01a77d0c0)
	pkg/mod/github.com/libp2p/go-libp2p-swarm@v0.5.3/swarm.go:553 +0x13e
github.com/libp2p/go-libp2p-swarm.(*Conn).doClose.func1(0xc06f36f4d0)
	pkg/mod/github.com/libp2p/go-libp2p-swarm@v0.5.3/swarm_conn.go:84 +0xa7
created by github.com/libp2p/go-libp2p-swarm.(*Conn).doClose
	pkg/mod/github.com/libp2p/go-libp2p-swarm@v0.5.3/swarm_conn.go:79 +0x16a

`,
			expected: []*Stack{
				{
					Number:   85751948,
					State:    "semacquire",
					WaitTime: 25 * time.Minute,
					Frames: []Frame{
						{
							Function: "sync.runtime_Semacquire",
							Params:   []string{"0xc099422a74"},
							File:     "/usr/local/go/src/runtime/sema.go",
							Line:     56,
							Entry:    69,
						},
						{
							Function: "sync.(*WaitGroup).Wait",
							Params:   []string{"0xc099422a74"},
							File:     "/usr/local/go/src/sync/waitgroup.go",
							Line:     130,
							Entry:    101,
						},
						{
							Function: "github.com/libp2p/go-libp2p-swarm.(*Swarm).notifyAll",
							Params: []string{
								"0xc000783380",
								"0xc01a77d0c0",
							},
							File:  "pkg/mod/github.com/libp2p/go-libp2p-swarm@v0.5.3/swarm.go",
							Line:  553,
							Entry: 318,
						},
						{
							Function: "github.com/libp2p/go-libp2p-swarm.(*Conn).doClose.func1",
							Params:   []string{"0xc06f36f4d0"},
							File:     "pkg/mod/github.com/libp2p/go-libp2p-swarm@v0.5.3/swarm_conn.go",
							Line:     84,
							Entry:    167,
						},
					},
					ThreadLocked: false,
					CreatedBy: CreatedBy{
						Function: "github.com/libp2p/go-libp2p-swarm.(*Conn).doClose",
						File:     "pkg/mod/github.com/libp2p/go-libp2p-swarm@v0.5.3/swarm_conn.go",
						Line:     79,
						Entry:    362,
					},
				},
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			reader := strings.NewReader(c.input)
			stacks, err := ParseStacks(reader, c.linePrefix)
			if err != nil {
				panic(err)
			}

			if !reflect.DeepEqual(c.expected, stacks) {
				expectedJSON, err := json.MarshalIndent(c.expected, "", "  ")
				if err != nil {
					panic(err)
				}
				gotJSON, err := json.MarshalIndent(stacks, "", "  ")
				if err != nil {
					panic(err)
				}
				t.Fatalf("expected:\n%v\ngot:\n%v", string(expectedJSON), string(gotJSON))
			}
		})
	}

}
