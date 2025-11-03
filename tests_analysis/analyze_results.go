package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

type rec struct {
	Operation    string  `json:"operation"`     // create_cart | add_items | get_cart
	ResponseTime float64 `json:"response_time"` // ms
	Success      bool    `json:"success"`
	StatusCode   int     `json:"status_code"`
	Timestamp    string  `json:"timestamp"`
}

type agg struct {
	countAll   int
	countOK    int
	rtAll      []float64 // all responses (incl. failures)
	rtSuccess  []float64 // success only
}

type dbStats struct {
	total map[string]*agg
	byOp  map[string]map[string]*agg
}

func pct(vs []float64, p float64) float64 {
	if len(vs) == 0 { return -1 }
	if p < 0 { p = 0 }
	if p > 100 { p = 100 }
	rank := p/100 * float64(len(vs)-1)
	i := int(rank)
	f := rank - float64(i)
	if i+1 < len(vs) {
		return vs[i] + f*(vs[i+1]-vs[i])
	}
	return vs[i]
}

func mean(vs []float64) float64 {
	if len(vs) == 0 { return -1 }
	var s float64
	for _, x := range vs { s += x }
	return s / float64(len(vs))
}

func loadAndAnalyze(filename string) (map[string]*agg, *agg, error) {
	f, err := os.Open(filename)
	if err != nil { return nil, nil, err }
	defer f.Close()

	var rows []rec
	if err := json.NewDecoder(f).Decode(&rows); err != nil { return nil, nil, err }

	byOp := map[string]*agg{
		"create_cart": {},
		"add_items":   {},
		"get_cart":    {},
	}
	total := &agg{}

	for _, r := range rows {
		a, ok := byOp[r.Operation]
		if !ok {
			a = &agg{}
			byOp[r.Operation] = a
		}
		a.countAll++
		a.rtAll = append(a.rtAll, r.ResponseTime)
		if r.Success {
			a.countOK++
			a.rtSuccess = append(a.rtSuccess, r.ResponseTime)
		}
		total.countAll++
		total.rtAll = append(total.rtAll, r.ResponseTime)
		if r.Success {
			total.countOK++
			total.rtSuccess = append(total.rtSuccess, r.ResponseTime)
		}
	}

	// Sort for percentiles
	for _, a := range byOp {
		sort.Float64s(a.rtSuccess)
		sort.Float64s(a.rtAll)
	}
	sort.Float64s(total.rtSuccess)
	sort.Float64s(total.rtAll)

	return byOp, total, nil
}

func printSingleAnalysis(name string, byOp map[string]*agg, total *agg) {
	// Verify counts
	expect := map[string]int{"create_cart": 50, "add_items": 50, "get_cart": 50}
	ok150 := true
	for k, want := range expect {
		if byOp[k].countAll != want {
			ok150 = false
		}
	}

	fmt.Printf("\n=== %s Analysis ===\n", name)
	fmt.Printf("Records: %d  (create=%d, add=%d, get=%d)\n",
		total.countAll, byOp["create_cart"].countAll, byOp["add_items"].countAll, byOp["get_cart"].countAll)
	if !ok150 {
		fmt.Println("⚠️  WARNING: counts are not 50/50/50 — check your loader run.")
	}

	fmt.Println()
	fmt.Printf("%-30s | %12s\n", "Metric", name)
	fmt.Println(strings.Repeat("-", 45))
	fmt.Printf("%-30s | %10.2f ms\n", "Avg Response Time", mean(total.rtSuccess))
	fmt.Printf("%-30s | %10.2f ms\n", "P50 Response Time", pct(total.rtSuccess, 50))
	fmt.Printf("%-30s | %10.2f ms\n", "P95 Response Time", pct(total.rtSuccess, 95))
	fmt.Printf("%-30s | %10.2f ms\n", "P99 Response Time", pct(total.rtSuccess, 99))
	fmt.Printf("%-30s | %11.2f%%\n", "Success Rate", 100*float64(total.countOK)/float64(total.countAll))
	fmt.Printf("%-30s | %12d\n", "Total Operations", total.countAll)

	fmt.Println("\nPer-Operation Breakdown:")
	fmt.Printf("%-15s | %12s | %12s | %12s\n", "Operation", "Avg (ms)", "P50 (ms)", "P95 (ms)")
	fmt.Println(strings.Repeat("-", 60))
	for _, k := range []string{"create_cart", "add_items", "get_cart"} {
		a := byOp[k]
		fmt.Printf("%-15s | %12.2f | %12.2f | %12.2f\n",
			k, mean(a.rtSuccess), pct(a.rtSuccess, 50), pct(a.rtSuccess, 95))
	}
}

func printComparison(mysqlByOp map[string]*agg, mysqlTotal *agg,
	dynamoByOp map[string]*agg, dynamoTotal *agg) {

	fmt.Println("\n\n" + strings.Repeat("=", 80))
	fmt.Println("=== MySQL vs DynamoDB Performance Comparison ===")
	fmt.Println(strings.Repeat("=", 80))

	// Overall comparison
	fmt.Println("\n📊 OVERALL PERFORMANCE:")
	fmt.Printf("%-30s | %12s | %12s | %15s\n", "Metric", "MySQL", "DynamoDB", "Difference")
	fmt.Println(strings.Repeat("-", 75))

	printMetricRow("Avg Response Time (ms)", mean(mysqlTotal.rtSuccess), mean(dynamoTotal.rtSuccess))
	printMetricRow("P50 Response Time (ms)", pct(mysqlTotal.rtSuccess, 50), pct(dynamoTotal.rtSuccess, 50))
	printMetricRow("P95 Response Time (ms)", pct(mysqlTotal.rtSuccess, 95), pct(dynamoTotal.rtSuccess, 95))
	printMetricRow("P99 Response Time (ms)", pct(mysqlTotal.rtSuccess, 99), pct(dynamoTotal.rtSuccess, 99))

	mysqlSuccessRate := 100 * float64(mysqlTotal.countOK) / float64(mysqlTotal.countAll)
	dynamoSuccessRate := 100 * float64(dynamoTotal.countOK) / float64(dynamoTotal.countAll)
	fmt.Printf("%-30s | %11.2f%% | %11.2f%% | %+14.2f%%\n",
		"Success Rate", mysqlSuccessRate, dynamoSuccessRate, dynamoSuccessRate-mysqlSuccessRate)

	// Per-operation comparison
	operations := []string{"create_cart", "add_items", "get_cart"}

	fmt.Println("\n\n🔍 PER-OPERATION COMPARISON:")
	for _, op := range operations {
		mysqlOp := mysqlByOp[op]
		dynamoOp := dynamoByOp[op]

		fmt.Printf("\n--- %s ---\n", op)
		fmt.Printf("%-25s | %12s | %12s | %15s\n", "Metric", "MySQL", "DynamoDB", "Difference")
		fmt.Println(strings.Repeat("-", 70))

		printMetricRow("Avg Response Time (ms)", mean(mysqlOp.rtSuccess), mean(dynamoOp.rtSuccess))
		printMetricRow("P50 (ms)", pct(mysqlOp.rtSuccess, 50), pct(dynamoOp.rtSuccess, 50))
		printMetricRow("P95 (ms)", pct(mysqlOp.rtSuccess, 95), pct(dynamoOp.rtSuccess, 95))
		printMetricRow("P99 (ms)", pct(mysqlOp.rtSuccess, 99), pct(dynamoOp.rtSuccess, 99))

		mysqlOpSuccess := 100 * float64(mysqlOp.countOK) / float64(mysqlOp.countAll)
		dynamoOpSuccess := 100 * float64(dynamoOp.countOK) / float64(dynamoOp.countAll)
		fmt.Printf("%-25s | %11.2f%% | %11.2f%% | %+14.2f%%\n",
			"Success Rate", mysqlOpSuccess, dynamoOpSuccess, dynamoOpSuccess-mysqlOpSuccess)
	}

	// Winner summary
	fmt.Println("\n\n🏆 WINNER SUMMARY:")
	fmt.Println(strings.Repeat("-", 60))

	mysqlWins := 0
	dynamoWins := 0
	ties := 0

	for _, op := range operations {
		mysqlAvg := mean(mysqlByOp[op].rtSuccess)
		dynamoAvg := mean(dynamoByOp[op].rtSuccess)
		diff := dynamoAvg - mysqlAvg
		pctDiff := (diff / mysqlAvg) * 100

		winner := ""
		if diff < -1 {
			winner = "✅ DynamoDB"
			dynamoWins++
		} else if diff > 1 {
			winner = "✅ MySQL"
			mysqlWins++
		} else {
			winner = "🤝 Tie"
			ties++
		}

		fmt.Printf("%-15s: %s (%.1f%% difference)\n", op, winner, pctDiff)
	}

	fmt.Printf("\nOverall: MySQL wins %d, DynamoDB wins %d, Ties %d\n", mysqlWins, dynamoWins, ties)
}

func printMetricRow(name string, mysql, dynamo float64) {
	diff := dynamo - mysql
	pct := (diff / mysql) * 100
	sign := ""
	emoji := ""

	if diff < 0 {
		sign = fmt.Sprintf("%.2f (%.1f%%)", diff, pct)
		emoji = "✅"
	} else if diff > 0 {
		sign = fmt.Sprintf("+%.2f (+%.1f%%)", diff, pct)
		emoji = "⚠️"
	} else {
		sign = "0.00"
		emoji = "="
	}

	fmt.Printf("%-30s | %10.2f ms | %10.2f ms | %s %s\n", name, mysql, dynamo, sign, emoji)
}

func saveCombinedResults(mysqlByOp map[string]*agg, mysqlTotal *agg,
	dynamoByOp map[string]*agg, dynamoTotal *agg) error {

	combined := map[string]interface{}{
		"mysql": map[string]interface{}{
			"overall": map[string]interface{}{
				"avg":          mean(mysqlTotal.rtSuccess),
				"p50":          pct(mysqlTotal.rtSuccess, 50),
				"p95":          pct(mysqlTotal.rtSuccess, 95),
				"p99":          pct(mysqlTotal.rtSuccess, 99),
				"success_rate": 100 * float64(mysqlTotal.countOK) / float64(mysqlTotal.countAll),
				"total_ops":    mysqlTotal.countAll,
			},
			"create_cart": map[string]interface{}{
				"avg": mean(mysqlByOp["create_cart"].rtSuccess),
				"p50": pct(mysqlByOp["create_cart"].rtSuccess, 50),
				"p95": pct(mysqlByOp["create_cart"].rtSuccess, 95),
			},
			"add_items": map[string]interface{}{
				"avg": mean(mysqlByOp["add_items"].rtSuccess),
				"p50": pct(mysqlByOp["add_items"].rtSuccess, 50),
				"p95": pct(mysqlByOp["add_items"].rtSuccess, 95),
			},
			"get_cart": map[string]interface{}{
				"avg": mean(mysqlByOp["get_cart"].rtSuccess),
				"p50": pct(mysqlByOp["get_cart"].rtSuccess, 50),
				"p95": pct(mysqlByOp["get_cart"].rtSuccess, 95),
			},
		},
		"dynamodb": map[string]interface{}{
			"overall": map[string]interface{}{
				"avg":          mean(dynamoTotal.rtSuccess),
				"p50":          pct(dynamoTotal.rtSuccess, 50),
				"p95":          pct(dynamoTotal.rtSuccess, 95),
				"p99":          pct(dynamoTotal.rtSuccess, 99),
				"success_rate": 100 * float64(dynamoTotal.countOK) / float64(dynamoTotal.countAll),
				"total_ops":    dynamoTotal.countAll,
			},
			"create_cart": map[string]interface{}{
				"avg": mean(dynamoByOp["create_cart"].rtSuccess),
				"p50": pct(dynamoByOp["create_cart"].rtSuccess, 50),
				"p95": pct(dynamoByOp["create_cart"].rtSuccess, 95),
			},
			"add_items": map[string]interface{}{
				"avg": mean(dynamoByOp["add_items"].rtSuccess),
				"p50": pct(dynamoByOp["add_items"].rtSuccess, 50),
				"p95": pct(dynamoByOp["add_items"].rtSuccess, 95),
			},
			"get_cart": map[string]interface{}{
				"avg": mean(dynamoByOp["get_cart"].rtSuccess),
				"p50": pct(dynamoByOp["get_cart"].rtSuccess, 50),
				"p95": pct(dynamoByOp["get_cart"].rtSuccess, 95),
			},
		},
	}

	outFile, err := os.Create("combined_results.json")
	if err != nil {
		return err
	}
	defer outFile.Close()

	encoder := json.NewEncoder(outFile)
	encoder.SetIndent("", "  ")
	return encoder.Encode(combined)
}

func main() {
	mysqlFile := "mysql_test_results.json"
	dynamoFile := "dynamodb_test_results.json"

	// Allow override via command line
	if len(os.Args) > 2 {
		mysqlFile = os.Args[1]
		dynamoFile = os.Args[2]
	}

	// Load MySQL results
	mysqlByOp, mysqlTotal, err := loadAndAnalyze(mysqlFile)
	if err != nil {
		fmt.Printf("❌ Error loading MySQL results from %s: %v\n", mysqlFile, err)
		os.Exit(1)
	}

	// Load DynamoDB results
	dynamoByOp, dynamoTotal, err := loadAndAnalyze(dynamoFile)
	if err != nil {
		fmt.Printf("❌ Error loading DynamoDB results from %s: %v\n", dynamoFile, err)
		os.Exit(1)
	}

	// Print individual analyses
	printSingleAnalysis("MySQL", mysqlByOp, mysqlTotal)
	printSingleAnalysis("DynamoDB", dynamoByOp, dynamoTotal)

	// Print comparison
	printComparison(mysqlByOp, mysqlTotal, dynamoByOp, dynamoTotal)

	// Save combined results
	if err := saveCombinedResults(mysqlByOp, mysqlTotal, dynamoByOp, dynamoTotal); err != nil {
		fmt.Printf("\n❌ Error saving combined results: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("\n✅ Combined results saved to combined_results.json")
}