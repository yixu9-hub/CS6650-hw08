package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

/************ 公共工具 ************/
type apiErr struct {
	Error   string `json:"error"`
	Message string `json:"message"`
	Details string `json:"details,omitempty"`
}
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if v != nil { _ = json.NewEncoder(w).Encode(v) }
}
func writeErr(w http.ResponseWriter, code int, e, msg string) {
	writeJSON(w, code, apiErr{Error: e, Message: msg})
}
func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" { return v }
	return def
}
func getenvInt(key string, def int) int {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 { return n }
	}
	return def
}

/************ MySQL 连接 & 建表 ************/
func openMySQLFromEnv() (*sql.DB, error) {
	host := os.Getenv("DB_HOST")
	user := os.Getenv("DB_USER")
	pass := os.Getenv("DB_PASS")
	name := os.Getenv("DB_NAME")
	if host == "" || user == "" || name == "" {
		return nil, fmt.Errorf("missing DB envs (DB_HOST/DB_USER/DB_NAME)")
	}
	dsn := fmt.Sprintf("%s:%s@tcp(%s:3306)/%s?parseTime=true&charset=utf8mb4,utf8", user, pass, host, name)
	db, err := sql.Open("mysql", dsn)
	if err != nil { return nil, err }
	db.SetMaxOpenConns(getenvInt("DB_MAX_OPEN_CONNS", 20))
	db.SetMaxIdleConns(getenvInt("DB_MAX_IDLE_CONNS", 10))
	db.SetConnMaxLifetime(5 * time.Minute)
	if err := db.Ping(); err != nil { return nil, err }
	return db, nil
}

func ensureCartSchema(db *sql.DB) error {
	ddls := []string{
		`CREATE TABLE IF NOT EXISTS carts (
			cart_id     INT AUTO_INCREMENT PRIMARY KEY,
			customer_id INT NOT NULL,
			status      ENUM('OPEN','CHECKED_OUT') NOT NULL DEFAULT 'OPEN',
			created_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			INDEX idx_carts_customer (customer_id, created_at)
		) ENGINE=InnoDB;`,
		`CREATE TABLE IF NOT EXISTS cart_items (
			cart_id    INT NOT NULL,
			product_id INT NOT NULL,
			quantity   INT NOT NULL,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			PRIMARY KEY (cart_id, product_id),
			CONSTRAINT fk_cart FOREIGN KEY (cart_id) REFERENCES carts(cart_id) ON DELETE CASCADE
		) ENGINE=InnoDB;`,
	}
	for _, s := range ddls {
		if _, err := db.Exec(s); err != nil { return err }
	}
	return nil
}

/************ Handlers: STEP I 三个端点 ************/

// 1) POST /shopping-carts  —— 创建购物车
type createCartReq struct{ CustomerID int `json:"customer_id"` }
type createCartResp struct{ ShoppingCartID int `json:"shopping_cart_id"` }

func createShoppingCartHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost { http.NotFound(w, r); return }
		var req createCartReq
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, 400, "INVALID_INPUT", "Invalid JSON"); return
		}
		if req.CustomerID < 1 {
			writeErr(w, 400, "INVALID_INPUT", "customer_id must be >= 1"); return
		}
		res, err := db.Exec(`INSERT INTO carts (customer_id) VALUES (?)`, req.CustomerID)
		if err != nil { writeErr(w, 500, "DB_ERROR", err.Error()); return }
		id64, _ := res.LastInsertId()
		writeJSON(w, 201, createCartResp{ShoppingCartID: int(id64)})
	}
}

// 2) POST /shopping-carts/{id}/items  —— 添加/更新/移除（quantity=0 => 删除）
type addItemsReq struct {
	ProductID int `json:"product_id"`
	Quantity  int `json:"quantity"`
}
func addItemsToCartHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost { http.NotFound(w, r); return }
		after := strings.TrimPrefix(r.URL.Path, "/shopping-carts/")
		parts := strings.Split(after, "/")
		if len(parts) < 2 || parts[1] != "items" { http.NotFound(w, r); return }

		cartID, err := strconv.Atoi(parts[0])
		if err != nil || cartID < 1 {
			writeErr(w, 400, "INVALID_INPUT", "shoppingCartId must be a positive integer"); return
		}
		var req addItemsReq
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, 400, "INVALID_INPUT", "Invalid JSON"); return
		}
		if req.ProductID < 1 || req.Quantity < 0 {
			writeErr(w, 400, "INVALID_INPUT", "product_id must be >=1 and quantity >=0"); return
		}

		tx, err := db.Begin()
		if err != nil { writeErr(w, 500, "DB_ERROR", err.Error()); return }
		defer tx.Rollback()

		// cart 存在性检查（避免向不存在购物车写入）
		var ok int
		if err := tx.QueryRow(`SELECT 1 FROM carts WHERE cart_id=?`, cartID).Scan(&ok); err != nil {
			if errors.Is(err, sql.ErrNoRows) { writeErr(w, 404, "NOT_FOUND", "shopping cart not found"); return }
			writeErr(w, 500, "DB_ERROR", err.Error()); return
		}

		// quantity==0 -> 删除该商品
		if req.Quantity == 0 {
			if _, err := tx.Exec(`DELETE FROM cart_items WHERE cart_id=? AND product_id=?`, cartID, req.ProductID); err != nil {
				writeErr(w, 500, "DB_ERROR", err.Error()); return
			}
			if _, err := tx.Exec(`UPDATE carts SET updated_at=NOW() WHERE cart_id=?`, cartID); err != nil {
				writeErr(w, 500, "DB_ERROR", err.Error()); return
			}
			if err := tx.Commit(); err != nil { writeErr(w, 500, "DB_ERROR", err.Error()); return }
			w.WriteHeader(204)
			return
		}

		// upsert：并发安全 & 幂等更新
		if _, err := tx.Exec(`
			INSERT INTO cart_items (cart_id, product_id, quantity)
			VALUES (?, ?, ?)
			ON DUPLICATE KEY UPDATE quantity=VALUES(quantity)
		`, cartID, req.ProductID, req.Quantity); err != nil {
			writeErr(w, 500, "DB_ERROR", err.Error()); return
		}
		if _, err := tx.Exec(`UPDATE carts SET updated_at=NOW() WHERE cart_id=?`, cartID); err != nil {
			writeErr(w, 500, "DB_ERROR", err.Error()); return
		}
		if err := tx.Commit(); err != nil { writeErr(w, 500, "DB_ERROR", err.Error()); return }
		w.WriteHeader(204)
	}
}

// 3) GET /shopping-carts/{id}  —— 高效整单查询（两次定点查询，<50ms）
type cartDTO struct {
	CartID     int       `json:"cart_id"`
	CustomerID int       `json:"customer_id"`
	Status     string    `json:"status"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}
type cartItemDTO struct {
	ProductID int `json:"product_id"`
	Quantity  int `json:"quantity"`
}
type getCartResp struct {
	Cart  cartDTO       `json:"cart"`
	Items []cartItemDTO `json:"items"`
}
func getShoppingCartHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet { http.NotFound(w, r); return }
		after := strings.TrimPrefix(r.URL.Path, "/shopping-carts/")
		if after == "" || strings.Contains(after, "/") { http.NotFound(w, r); return }

		cartID, err := strconv.Atoi(after)
		if err != nil || cartID < 1 {
			writeErr(w, 400, "INVALID_INPUT", "shoppingCartId must be positive int"); return
		}

		// 1) 主键查 cart
		var c cartDTO
		err = db.QueryRow(`SELECT cart_id, customer_id, status, created_at, updated_at FROM carts WHERE cart_id=?`, cartID).
			Scan(&c.CartID, &c.CustomerID, &c.Status, &c.CreatedAt, &c.UpdatedAt)
		if errors.Is(err, sql.ErrNoRows) { writeErr(w, 404, "NOT_FOUND", "cart not found"); return }
		if err != nil { writeErr(w, 500, "DB_ERROR", err.Error()); return }

		// 2) 覆盖索引/主键查 items（最多 50）
		rows, err := db.Query(`SELECT product_id, quantity FROM cart_items WHERE cart_id=? LIMIT 50`, cartID)
		if err != nil { writeErr(w, 500, "DB_ERROR", err.Error()); return }
		defer rows.Close()

		items := make([]cartItemDTO, 0, 16)
		for rows.Next() {
			var it cartItemDTO
			if err := rows.Scan(&it.ProductID, &it.Quantity); err != nil { writeErr(w, 500, "DB_ERROR", err.Error()); return }
			items = append(items, it)
		}
		if err := rows.Err(); err != nil { writeErr(w, 500, "DB_ERROR", err.Error()); return }
		writeJSON(w, 200, getCartResp{Cart: c, Items: items})
	}
}

/************ 健康检查 ************/
func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

/************ main ************/
func main() {
	// Check DB_BACKEND environment variable to determine which backend to use
	backend := getenv("DB_BACKEND", "mysql") // default to mysql for backward compatibility
	
	mux := http.NewServeMux()
	mux.HandleFunc("/health", healthHandler)

	if backend == "dynamodb" {
		// DynamoDB backend initialization
		ddb, err := initDynamoDB()
		if err != nil { panic(fmt.Errorf("init DynamoDB: %w", err)) }
		
		mux.HandleFunc("/shopping-carts", createShoppingCartHandlerDynamo(ddb)) // POST
		mux.HandleFunc("/shopping-carts/", func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.Method == http.MethodGet && !strings.Contains(strings.TrimPrefix(r.URL.Path, "/shopping-carts/"), "/"):
				getShoppingCartHandlerDynamo(ddb)(w, r); return
			case strings.HasSuffix(r.URL.Path, "/items"):
				addItemsToCartHandlerDynamo(ddb)(w, r); return
			default:
				http.NotFound(w, r); return
			}
		})
	} else {
		// MySQL backend initialization (default)
		db, err := openMySQLFromEnv()
		if err != nil { panic(fmt.Errorf("open DB: %w", err)) }
		if err := ensureCartSchema(db); err != nil { panic(fmt.Errorf("ensure schema: %w", err)) }
		
		mux.HandleFunc("/shopping-carts", createShoppingCartHandler(db)) // POST
		mux.HandleFunc("/shopping-carts/", func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.Method == http.MethodGet && !strings.Contains(strings.TrimPrefix(r.URL.Path, "/shopping-carts/"), "/"):
				getShoppingCartHandler(db)(w, r); return
			case strings.HasSuffix(r.URL.Path, "/items"):
				addItemsToCartHandler(db)(w, r); return
			default:
				http.NotFound(w, r); return
			}
		})
	}

	port := getenvInt("PORT", 8080)
	srv := &http.Server{ Addr: fmt.Sprintf(":%d", port), Handler: mux }
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		panic(err)
	}
}