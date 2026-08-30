package exportfile

import (
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

// THE HOLDER EXPORT'S SIZE, MEASURED (#530, parent #518, ADR 0065).
//
// ADR 0065 set the fifty-thousand-Ticket cap as an explicit JUDGEMENT and named
// this benchmark as the thing that settles it. What has to be measured is not
// "does BuildHolderExport finish" but whether one request's worth of work fits
// inside the deployed Cloud Run request timeout (api_request_timeout_seconds,
// 300s) and — the tighter bound — inside the deployed memory limit (api_memory,
// 512Mi), because the workbook is buffered whole in memory and an OOM takes the
// PROCESS down rather than the request.
//
// IT IS A Benchmark AND NOT A Test, deliberately. `go test ./...` does not run
// benchmarks without -bench, so this cannot slow the ordinary suite by a
// millisecond, and it stays rerunnable by name. Rerun it with:
//
//	go test ./internal/sales/exportfile/ -run XXX -bench BenchmarkHolderExport -benchtime 1x
//
// WIDTH IS THE POINT, which is why the fixture below is a shape and not a row
// count. Option columns fan a single multiple-choice question out to one column
// per Option — retired Options included, since a retired Option keeps its column
// precisely so the Tickets that chose it keep reading — and excelize holds every
// written cell as a struct in a slice-of-rows until WriteToBuffer serialises the
// lot. Cells, not rows, are the unit of both cost centres, so a benchmark run
// against a narrow fixture would measure nothing the cap is about.

// benchQuestions is the WORST-CASE-BUT-PLAUSIBLE Ticket Question shape this
// benchmark measures against: eight multiple-choice questions of twenty-five
// Options each, and six questions of the kinds that take one column.
//
// THE SHAPE IS CHOSEN FROM WHAT AN ORGANIZATION COULD ACTUALLY DO, not from what
// the schema permits — the schema permits far worse, and a fixture nobody could
// reach would let a real Event fail a cap this measurement had blessed. A
// multi-day conference selling one Event asks a workshop track (twenty-odd
// sessions), a dietary requirement, a t-shirt size, an arrival day, a merch
// choice and so on; twenty-five Options is one such list after two years of
// additions and retirements. Retired Options are counted IN this number, since
// they take columns like any other.
//
// 8*25 + 6 = 206 question columns, over the thirteen fixed and Holder columns:
// 219 columns, and at fifty thousand rows just under eleven million cells. A
// WIDER SHAPE WOULD BE WORSE and this benchmark's verdict does not cover it: an
// Organization that asks fifteen multiple-choice questions of forty Options is
// building a sheet three times this one, and the cap that holds here would not
// hold there.
//
// THE SHAPE IS OVERRIDABLE — HOLDER_EXPORT_BENCH_SHAPE="choice:options:plain",
// e.g. "0:0:0" for a sheet with no question columns at all — because the useful
// question is not only "does this shape fit" but "how much of the cost is
// width", and the narrow shape is the floor a cap has to clear even for an Event
// that asks nothing.
const (
	benchChoiceQuestions = 8
	benchOptionsPer      = 25
	benchPlainQuestions  = 6
)

// benchShape is the fixture's width, from the environment or the constants
// above.
func benchShape() (choice, options, plain int) {
	choice, options, plain = benchChoiceQuestions, benchOptionsPer, benchPlainQuestions
	parts := strings.Split(os.Getenv("HOLDER_EXPORT_BENCH_SHAPE"), ":")
	if len(parts) != 3 {
		return choice, options, plain
	}
	nums := make([]int, 0, 3)
	for _, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 {
			return choice, options, plain
		}
		nums = append(nums, n)
	}
	return nums[0], nums[1], nums[2]
}

// benchRoster builds a roster of the given height at the fixture's width, with
// EVERY question answered on every row.
//
// Every question answered is the worst case and it is also the realistic one for
// a required question: an unanswered question writes no cells at all (CellsFor
// skips it), so a fixture of half-answered rows would quietly measure a narrower
// sheet than its header claims.
func benchRoster(rows int) (HolderRoster, HolderInfo) {
	choiceCount, optionsPer, plainCount := benchShape()
	questions := make([]QuestionColumn, 0, choiceCount+plainCount)
	for q := 0; q < choiceCount; q++ {
		options := make([]OptionColumn, 0, optionsPer)
		for o := 0; o < optionsPer; o++ {
			options = append(options, OptionColumn{
				ID:    fmt.Sprintf("q%d-opt%d", q, o),
				Label: fmt.Sprintf("Question %d option %d", q, o),
			})
		}
		questions = append(questions, QuestionColumn{
			ID:      fmt.Sprintf("q%d", q),
			Label:   fmt.Sprintf("Multiple choice question %d", q),
			Options: options,
		})
	}
	for p := 0; p < plainCount; p++ {
		questions = append(questions, QuestionColumn{
			ID:    fmt.Sprintf("p%d", p),
			Label: fmt.Sprintf("Plain question %d", p),
		})
	}

	answered := time.Date(2026, 7, 1, 9, 0, 0, 0, time.UTC)
	tickets := make([]HolderRow, 0, rows)
	for i := 0; i < rows; i++ {
		answers := make(map[string]Answer, len(questions))
		for q := 0; q < choiceCount; q++ {
			// One Option chosen and the other twenty-four written FALSE, which is
			// what a fanned-out Answer costs: the whole block is written, because a
			// Ticket that answered says FALSE to what it did not pick.
			answers[fmt.Sprintf("q%d", q)] = Answer{Chosen: []string{fmt.Sprintf("q%d-opt%d", q, i%max(optionsPer, 1))}}
		}
		text := "Some answer text " + strconv.Itoa(i)
		number := float64(i)
		checked := i%2 == 0
		// The plain questions round-robin through the kinds, so a narrowed shape
		// still measures a mix of cell types rather than six copies of one.
		for p := 0; p < plainCount; p++ {
			key := fmt.Sprintf("p%d", p)
			switch p % 4 {
			case 0, 1:
				answers[key] = Answer{Text: &text}
			case 2:
				answers[key] = Answer{Number: &number}
			case 3:
				answers[key] = Answer{Date: &answered}
			}
		}
		if plainCount > 4 {
			answers["p4"] = Answer{Checked: &checked}
		}

		tickets = append(tickets, HolderRow{
			ConfirmationRef:   fmt.Sprintf("MT-%06d", i),
			SoldAt:            answered.Add(time.Duration(i) * time.Second),
			Channel:           "online",
			TicketTypeName:    "General Admission",
			Ordinal:           i%4 + 1,
			CustomerFirstName: "Buyer" + strconv.Itoa(i),
			CustomerLastName:  "Surname" + strconv.Itoa(i),
			CustomerEmail:     fmt.Sprintf("buyer%d@example.com", i),
			AssignmentState:   "accepted",
			HolderFirstName:   "Holder" + strconv.Itoa(i),
			HolderLastName:    "Surname" + strconv.Itoa(i),
			HolderEmail:       fmt.Sprintf("holder%d@example.com", i),
			Answers:           answers,
		})
	}

	return HolderRoster{
			Questions:  questions,
			Assignment: true,
			Tickets:    tickets,
		}, HolderInfo{
			EventName:   "Benchmark Fest",
			GeneratedAt: answered,
			Filters:     HolderFilters{Sort: "sold_at", Dir: "asc"},
		}
}

// BenchmarkHolderExportBuild measures the workbook build alone at several
// heights, so the row at which the cap stops fitting can be READ OFF rather than
// guessed at. It reports wall time, the bytes the process took from the OS
// (Sys), and the peak heap, since the memory limit and not the timeout is the
// bound that is expected to bite.
//
// The heights are separate sub-benchmarks rather than one: what is wanted is the
// SHAPE of the curve, and a single point at fifty thousand cannot say what
// number to come down to if it fails.
func BenchmarkHolderExportBuild(b *testing.B) {
	for _, rows := range benchHeights() {
		b.Run(strconv.Itoa(rows), func(b *testing.B) {
			roster, info := benchRoster(rows)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				runtime.GC()
				var before, after runtime.MemStats
				runtime.ReadMemStats(&before)
				start := time.Now()
				data, err := BuildHolderExport(roster, time.UTC, info)
				elapsed := time.Since(start)
				runtime.ReadMemStats(&after)
				if err != nil {
					b.Fatalf("build %d rows: %v", rows, err)
				}
				b.ReportMetric(elapsed.Seconds(), "s/build")
				b.ReportMetric(float64(after.TotalAlloc-before.TotalAlloc)/(1<<20), "MiB-allocated")
				b.ReportMetric(float64(after.Sys)/(1<<20), "MiB-sys")
				b.ReportMetric(float64(after.HeapAlloc)/(1<<20), "MiB-heap-after")
				b.ReportMetric(float64(len(data))/(1<<20), "MiB-xlsx")
			}
		})
	}
}

// benchHeights is which row counts to measure. Overridable so a probe run can be
// cheap: HOLDER_EXPORT_BENCH_ROWS=1000,5000.
func benchHeights() []int {
	if raw := os.Getenv("HOLDER_EXPORT_BENCH_ROWS"); raw != "" {
		out := []int{}
		for _, part := range strings.Split(raw, ",") {
			n, err := strconv.Atoi(part)
			if err == nil && n > 0 {
				out = append(out, n)
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	return []int{1_000, 5_000, 10_000, 25_000, 50_000}
}
