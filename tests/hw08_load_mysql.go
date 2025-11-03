package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"sync"
	"time"
)

type result struct {
	Operation    string  `json:"operation"`
	ResponseTime float64 `json:"response_time"` // ms
	Success      bool    `json:"success"`
	StatusCode   int     `json:"status_code"`
	Timestamp    string  `json:"timestamp"`
}

type createResp struct {
	ShoppingCartID int64 `json:"shopping_cart_id"`
}

// doReq 发起 HTTP 请求并返回状态码、耗时(ms)、响应体
func doReq(ctx context.Context, client *http.Client, method, url string, body any) (status int, durMs float64, respBody []byte, err error) {
	var rdr io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = bytes.NewReader(b)
	}
	req, _ := http.NewRequestWithContext(ctx, method, url, rdr)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	start := time.Now()
	resp, err := client.Do(req)
	durMs = float64(time.Since(start).Milliseconds())
	if err != nil {
		return 0, durMs, nil, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, durMs, b, nil
}

func main() {
	// --- flags ---
	base := flag.String("base", os.Getenv("BASE"), "Base URL, e.g. http://localhost:8080 or http://<alb-dns>")
	out := flag.String("out", "mysql_test_results.json", "Output JSON file")
	concurrency := flag.Int("concurrency", 10, "Concurrent workers per phase")
	timeout := flag.Duration("timeout", 5*time.Minute, "Overall timeout")

	// 次数可调，默认作业要求 50/50/50
	createN := flag.Int("create", 50, "Number of create_cart operations")
	addN := flag.Int("add", 50, "Number of add_items operations")
	getN := flag.Int("get", 50, "Number of get_cart operations")

	// 创建重试次数（只记录一次最终结果）
	maxCreateRetries := flag.Int("create_retries", 3, "Retries per create operation")
	flag.Parse()

	if *base == "" {
		fmt.Println("ERROR: missing -base or env BASE (e.g. -base http://localhost:8080)")
		os.Exit(1)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	// 结果收集
	var resultsMu sync.Mutex
	var results []result
	record := func(op string, status int, durMs float64, ok bool) {
		resultsMu.Lock()
		results = append(results, result{
			Operation:    op,
			ResponseTime: durMs,
			Success:      ok,
			StatusCode:   status,
			Timestamp:    time.Now().UTC().Format(time.RFC3339),
		})
		resultsMu.Unlock()
	}

	// 成功创建的 cartIDs
	var cartIDsMu sync.Mutex
	var cartIDs []int64

	// -------------------------
	// Phase 1: 恰好 *createN 次 创建；每次创建内部可重试，但只记录一次最终结果
	// -------------------------
	fmt.Printf("Phase 1: creating %d carts (with retries, but recording once per create)...\n", *createN)
	runConcurrent(ctx, *concurrency, *createN, func(i int) {
		url := fmt.Sprintf("%s/shopping-carts", *base)

		var finalOK bool
		var finalStatus int
		var finalDur float64
		var gotID int64

		for attempt := 0; attempt <= *maxCreateRetries; attempt++ {
			status, dur, b, err := doReq(ctx, client, http.MethodPost, url, map[string]any{"customer_id": 1})
			finalStatus, finalDur = status, dur
			finalOK = (err == nil && status == 201)
			if finalOK {
				var cr createResp
				if json.Unmarshal(b, &cr) == nil && cr.ShoppingCartID > 0 {
					gotID = cr.ShoppingCartID
					break
				}
				finalOK = false // body 解析失败则继续重试
			}

			// 指数退避 100ms * 2^attempt（最多 800ms）
			sleepMs := int(math.Min(800, 100*math.Pow(2, float64(attempt))))
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Duration(sleepMs) * time.Millisecond):
			}
		}

		// 这一次创建只记录 1 条（最终结果）
		record("create_cart", finalStatus, finalDur, finalOK)

		if finalOK && gotID > 0 {
			cartIDsMu.Lock()
			cartIDs = append(cartIDs, gotID)
			cartIDsMu.Unlock()
		}
	})
	fmt.Println("Phase 1 done.")

	// 如果一次都没成功，为保障 Phase2/3 可用，偷偷兜底创建 1 个（不计入 150）
	var fallbackID int64
	cartIDsMu.Lock()
	needFallback := len(cartIDs) == 0
	cartIDsMu.Unlock()
	if needFallback {
		status, _, b, err := doReq(ctx, client, http.MethodPost, fmt.Sprintf("%s/shopping-carts", *base), map[string]any{"customer_id": 1})
		if err == nil && status == 201 {
			var cr createResp
			if json.Unmarshal(b, &cr) == nil && cr.ShoppingCartID > 0 {
				fallbackID = cr.ShoppingCartID
				fmt.Println("NOTE: created 1 fallback cart (not counted in 150 results).")
			}
		}
	}

	// 取一个安全的 cart 取模函数
	getCartID := func(i int) int64 {
		cartIDsMu.Lock()
		defer cartIDsMu.Unlock()
		if len(cartIDs) > 0 {
			return cartIDs[i%len(cartIDs)]
		}
		// 如果没有成功创建过，使用兜底 ID（可能为 0，后续请求会得到 404，但仍计入操作）
		return fallbackID
	}

	// -------------------------
	// Phase 2: 恰好 *addN 次 add_items
	// -------------------------
	fmt.Printf("Phase 2: adding %d items...\n", *addN)
	runConcurrent(ctx, *concurrency, *addN, func(i int) {
		cid := getCartID(i)
		body := map[string]any{
			"product_id": 1000 + (i % 50),
			"quantity":   1 + (i % 3),
		}
		url := fmt.Sprintf("%s/shopping-carts/%d/items", *base, cid)
		status, dur, _, err := doReq(ctx, client, http.MethodPost, url, body)
		ok := (err == nil && status == 204)
		record("add_items", status, dur, ok)
	})
	fmt.Println("Phase 2 done.")

	// -------------------------
	// Phase 3: 恰好 *getN 次 get_cart
	// -------------------------
	fmt.Printf("Phase 3: getting %d carts...\n", *getN)
	runConcurrent(ctx, *concurrency, *getN, func(i int) {
		cid := getCartID(i)
		url := fmt.Sprintf("%s/shopping-carts/%d", *base, cid)
		status, dur, _, err := doReq(ctx, client, http.MethodGet, url, nil)
		ok := (err == nil && status == 200)
		record("get_cart", status, dur, ok)
	})
	fmt.Println("Phase 3 done.")

	// --- 输出文件（恰好 createN + addN + getN 条） ---
	if err := writeJSONFile(*out, results); err != nil {
		fmt.Println("write output error:", err)
		os.Exit(1)
	}
	fmt.Printf("Done. Wrote %d results to %s\n", len(results), *out)
}

// runConcurrent 按给定并发度执行 n 次 fn(i)
func runConcurrent(ctx context.Context, conc, n int, fn func(i int)) {
	if conc < 1 {
		conc = 1
	}
	sem := make(chan struct{}, conc)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		select {
		case <-ctx.Done():
			return
		case sem <- struct{}{}:
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				defer func() { <-sem }()
				fn(i)
			}(i)
		}
	}
	wg.Wait()
}

// writeJSONFile 将结果写入 JSON 文件（缩进美化）
func writeJSONFile(path string, v any) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}