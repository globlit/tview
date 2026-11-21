package tview

import (
	"fmt"
	"math/rand"
	"reflect"
	"strings"
	"testing"

	"github.com/derailed/tcell/v2"
)

var reindexWords = []string{
	"INFO", "WARN", "ERROR", "seq=42", "GET", "/api/v1/orders", "status=200",
	"latency=12ms", "héllo", "日本語", "🙂", "a\tb", "x,y.z", "   ", "[red::b]", "[-:-:-]",
	"[#00ff00:black:u]", "[yellow::]", "[::-]", `["r1"]`, `[""]`, "[foo[]", "[notatag]",
	"veryveryveryverylongtokenwithoutanyspacesatallthatmustbesplit",
}

func randomText(r *rand.Rand) string {
	var sb strings.Builder
	for n := r.Intn(12); n > 0; n-- {
		sb.WriteString(reindexWords[r.Intn(len(reindexWords))])
		if r.Intn(3) > 0 {
			sb.WriteByte(' ')
		}
	}
	if r.Intn(4) > 0 {
		sb.WriteByte('\n')
	}

	return sb.String()
}

type reindexCase struct {
	wrap, wordWrap, colors, regions bool
	maxLines                        int
}

func (c reindexCase) view() *TextView {
	t := NewTextView().
		SetWrap(c.wrap).
		SetWordWrap(c.wordWrap).
		SetDynamicColors(c.colors).
		SetRegions(c.regions)
	if c.maxLines > 0 {
		t.SetMaxLines(c.maxLines)
	}

	return t
}

func screenText(s tcell.SimulationScreen) string {
	cells, w, _ := s.GetContents()
	var sb strings.Builder
	for i, c := range cells {
		if i > 0 && i%w == 0 {
			sb.WriteByte('\n')
		}
		sb.WriteString(fmt.Sprintf("%s|%v;", string(c.Runes), c.Style))
	}

	return sb.String()
}

func newSim(t *testing.T) tcell.SimulationScreen {
	t.Helper()
	s := tcell.NewSimulationScreen("UTF-8")
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	s.SetSize(60, 12)

	return s
}

func snapshot(tv *TextView) (buffer []string, index []textViewIndex) {
	for _, l := range tv.buffer {
		buffer = append(buffer, string(l))
	}
	for _, l := range tv.index {
		index = append(index, *l)
	}

	return buffer, index
}

// Compare incremental and full re-index on random writes split mid rune and
// mid tag, across wrap, tag and maxLines settings.
func TestTextViewIncrementalReindex(t *testing.T) {
	var cases []reindexCase
	for _, wrap := range []bool{false, true} {
		for _, wordWrap := range []bool{false, true} {
			for _, colors := range []bool{false, true} {
				for _, regions := range []bool{false, true} {
					for _, maxLines := range []int{0, 1, 7, 40} {
						cases = append(cases, reindexCase{wrap, wordWrap, colors, regions, maxLines})
					}
				}
			}
		}
	}

	for _, c := range cases {
		c := c
		t.Run(fmt.Sprintf("%+v", c), func(t *testing.T) {
			r := rand.New(rand.NewSource(int64(len(fmt.Sprint(c)))))
			got, want := c.view(), c.view()
			sGot, sWant := newSim(t), newSim(t)
			width := 30
			incremental := 0
			var ops []string
			fail := func(format string, args ...interface{}) {
				if len(ops) > 12 {
					ops = ops[len(ops)-12:]
				}
				t.Fatalf(format+"\nlast ops: %q", append(args, ops)...)
			}
			for step := 0; step < 400; step++ {
				switch op := r.Intn(40); {
				case op == 0:
					ops = append(ops, "clear")
					got.Clear()
					want.Clear()
				case op == 1:
					width = 5 + r.Intn(40)
					ops = append(ops, fmt.Sprintf("width=%d", width))
				case op == 2 && c.regions:
					got.Highlight("r1")
					want.Highlight("r1")
				case op == 3:
					got.Highlight()
					want.Highlight()
				default:
					text := []byte(randomText(r))
					for len(text) > 0 {
						n := 1 + r.Intn(len(text))
						ops = append(ops, "write "+string(text[:n]))
						_, _ = got.Write(text[:n])
						_, _ = want.Write(text[:n])
						text = text[n:]
					}
				}
				if r.Intn(3) == 0 {
					continue // several writes between frames
				}
				if got.reuse != nil {
					incremental++
				}
				ops = append(ops, fmt.Sprintf("draw reuse=%t from=%d shift=%d", got.reuse != nil, got.reuseFrom, got.reuseShift))
				// Oracle: stock tview, full re-index only.
				want.reuse = nil
				got.SetRect(0, 0, width, 10)
				want.SetRect(0, 0, width, 10)
				sGot.Clear()
				sWant.Clear()
				got.Draw(sGot)
				want.Draw(sWant)

				gb, gi := snapshot(got)
				wb, wi := snapshot(want)
				if !reflect.DeepEqual(gb, wb) {
					fail("step %d: buffers differ\n got %q\nwant %q", step, gb, wb)
				}
				if !reflect.DeepEqual(gi, wi) {
					fail("step %d: indexes differ\n got %+v\nwant %+v", step, gi, wi)
				}
				if got.longestLine != want.longestLine || got.lineOffset != want.lineOffset {
					fail("step %d: longest %d/%d offset %d/%d", step, got.longestLine, want.longestLine, got.lineOffset, want.lineOffset)
				}
				if a, b := screenText(sGot), screenText(sWant); a != b {
					fail("step %d: screens differ\n got %s\nwant %s", step, a, b)
				}
			}
			if incremental == 0 {
				t.Fatal("the incremental path was never exercised")
			}
		})
	}
}

func TestStringWidthASCIIFastPath(t *testing.T) {
	for _, s := range []string{"", "abc", "status=200 latency=12ms", "~!@#$%^&*()_+{}|:<>?"} {
		g := 0
		for _, r := range s {
			_ = r
			g++
		}
		if got := stringWidth(s); got != g {
			t.Errorf("%q: got %d, want %d", s, got, g)
		}
	}
	// Non-ASCII: grapheme segmentation.
	for s, want := range map[string]int{"héllo": 5, "日本": 4, "é": 1, "a\x00b": 2} {
		if got := stringWidth(s); got != want {
			t.Errorf("%q: got %d, want %d", s, got, want)
		}
	}
}

func TestDecomposeTagsWithoutBrackets(t *testing.T) {
	for _, s := range []string{"plain text", "日本 🙂", ""} {
		ci, c, ri, r, ei, stripped, w := decomposeString(s, true, true)
		if ci != nil || c != nil || ri != nil || r != nil || ei != nil || stripped != s || w != stringWidth(s) {
			t.Errorf("%q: unexpected decomposition", s)
		}
	}
	_, _, _, _, _, stripped, w := decomposeString("[red::]a[foo[]b", true, false)
	if stripped != "a[foo]b" || w != 7 {
		t.Errorf("got %q (%d)", stripped, w)
	}
}

func benchLines(n int) []byte {
	var b []byte
	for i := 0; i < n; i++ {
		b = append(b, fmt.Sprintf("[yellow::]log-spam-67dc5dbcc8-2zf5n[-::] INFO seq=%d pod=log-spam-67dc5dbcc8-2zf5n GET /api/v1/orders status=200 latency=%dms user=u%05d msg=\"request handled by worker-3\"\n", i, i%900, i)...)
	}

	return b
}

// Simulate a log view: full buffer, append a batch, draw a frame.
func BenchmarkTextViewLogFrame(b *testing.B) {
	for _, wrap := range []bool{false, true} {
		b.Run(fmt.Sprintf("wrap=%t", wrap), func(b *testing.B) {
			tv := NewTextView().SetDynamicColors(true).SetWrap(wrap).SetMaxLines(5000)
			_, _ = tv.Write(benchLines(5000))
			s := tcell.NewSimulationScreen("UTF-8")
			_ = s.Init()
			s.SetSize(200, 50)
			tv.SetRect(0, 0, 200, 50)
			extra := benchLines(100)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, _ = tv.Write(extra)
				tv.Draw(s)
			}
		})
	}
}
