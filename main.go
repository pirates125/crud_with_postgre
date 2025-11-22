package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
)

type Item struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

var db *sql.DB

// writeJSONError: tutarlı JSON hata cevabı
func writeJSONError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"error": msg,
		"code":  code,
	})
}

func withTimeoutCtx(d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), d)
}

func RequestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		log.Printf("START -> %s %s", r.Method, r.URL.Path)
		next.ServeHTTP(w, r)
		log.Printf("END   -> %s (%v)", r.URL.Path, time.Since(start))
	})
}

func AuthGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") {
			writeJSONError(w, "Missing or invalid auth header!", http.StatusUnauthorized)
			return
		}
		token := strings.TrimSpace(strings.TrimPrefix(auth, "Bearer "))
		if token != "secret-token" {
			writeJSONError(w, "Invalid auth token!", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// DB helpers
func createItemDB(ctx context.Context, name string) (int, error) {
	var id int
	err := db.QueryRowContext(ctx, "INSERT INTO items (name) VALUES ($1) RETURNING id", name).Scan(&id)
	return id, err
}

func listItemsDB(ctx context.Context) ([]Item, error) {
	rows, err := db.QueryContext(ctx, "SELECT id, name FROM items ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var res []Item
	for rows.Next() {
		var it Item
		if err := rows.Scan(&it.ID, &it.Name); err != nil {
			return nil, err
		}
		res = append(res, it)
	}
	return res, rows.Err()
}

func getItemDB(ctx context.Context, id int) (Item, error) {
	var it Item
	err := db.QueryRowContext(ctx, "SELECT id, name FROM items WHERE id=$1", id).Scan(&it.ID, &it.Name)
	return it, err
}

func updateItemDB(ctx context.Context, id int, name string) error {
	res, err := db.ExecContext(ctx, "UPDATE items SET name=$1 WHERE id=$2", name, id)
	if err != nil {
		return err
	}
	aff, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if aff == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func deleteItemDB(ctx context.Context, id int) error {
	res, err := db.ExecContext(ctx, "DELETE FROM items WHERE id=$1", id)
	if err != nil {
		return err
	}
	aff, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if aff == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// Handlers
func listItems(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := withTimeoutCtx(2 * time.Second)
	defer cancel()

	items, err := listItemsDB(ctx)
	if err != nil {
		log.Println("listItems err:", err)
		writeJSONError(w, "Failed to list items.", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(items)
}

func getItem(w http.ResponseWriter, r *http.Request) {
	idParam := chi.URLParam(r, "id")
	id, err := strconv.Atoi(idParam)
	if err != nil {
		writeJSONError(w, "Invalid id", http.StatusBadRequest)
		return
	}
	ctx, cancel := withTimeoutCtx(2 * time.Second)
	defer cancel()

	it, err := getItemDB(ctx, id)
	if err != nil {
		if err == sql.ErrNoRows {
			writeJSONError(w, "Item not found!", http.StatusNotFound)
			return
		}
		log.Println("getItem err:", err)
		writeJSONError(w, "Internal Error!", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(it)
}

func createItem(w http.ResponseWriter, r *http.Request) {
	var newItem Item
	if err := json.NewDecoder(r.Body).Decode(&newItem); err != nil {
		writeJSONError(w, "Invalid json!", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(newItem.Name) == "" {
		writeJSONError(w, "Name is required!", http.StatusBadRequest)
		return
	}
	ctx, cancel := withTimeoutCtx(2 * time.Second)
	defer cancel()

	id, err := createItemDB(ctx, newItem.Name)
	if err != nil {
		log.Println("createItem err:", err)
		writeJSONError(w, "Failed to create!", http.StatusInternalServerError)
		return
	}

	newItem.ID = id
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Location", fmt.Sprintf("/items/%d", id))
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(newItem)
}

func updateItem(w http.ResponseWriter, r *http.Request) {
	idParam := chi.URLParam(r, "id")
	id, err := strconv.Atoi(idParam)
	if err != nil {
		writeJSONError(w, "Invalid id!", http.StatusBadRequest)
		return
	}
	var payload Item
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeJSONError(w, "Invalid json!", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(payload.Name) == "" {
		writeJSONError(w, "Name is required!", http.StatusBadRequest)
		return
	}
	ctx, cancel := withTimeoutCtx(2 * time.Second)
	defer cancel()

	if err := updateItemDB(ctx, id, payload.Name); err != nil {
		if err == sql.ErrNoRows {
			writeJSONError(w, "Item not found!", http.StatusNotFound)
			return
		}
		log.Println("update err:", err)
		writeJSONError(w, "Failed to update!", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func deleteItem(w http.ResponseWriter, r *http.Request) {
	idParam := chi.URLParam(r, "id")
	id, err := strconv.Atoi(idParam)
	if err != nil {
		writeJSONError(w, "Invalid id!", http.StatusBadRequest)
		return
	}
	ctx, cancel := withTimeoutCtx(2 * time.Second)
	defer cancel()

	if err := deleteItemDB(ctx, id); err != nil {
		if err == sql.ErrNoRows {
			writeJSONError(w, "Item not found!", http.StatusNotFound)
			return
		}
		log.Println("delete err:", err)
		writeJSONError(w, "Failed to delete!", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// getenvRequired: env yoksa fatal
func getenvRequired(k string) string {
	v := os.Getenv(k)
	if v == "" {
		log.Fatalf("environment variable %s is required but not set", k)
	}
	return v
}

func main() {
	// .env yükle (opsiyonel)
	_ = godotenv.Load() // hata sadece loglanır

	// debug - programın gördüğü env'ler
	log.Printf("DEBUG env: PGHOST='%s' PGPORT='%s' PGUSER='%s' PGDB='%s' ADDR='%s'\n",
		os.Getenv("PGHOST"), os.Getenv("PGPORT"), os.Getenv("PGUSER"), os.Getenv("PGDB"), os.Getenv("ADDR"))

	// burada env'ler required: eğer eksikse program kapanacak
	pgHost := getenvRequired("PGHOST")
	pgPort := getenvRequired("PGPORT")
	pgUser := getenvRequired("PGUSER")
	pgPass := getenvRequired("PGPASSWORD")
	pgDB := getenvRequired("PGDB")
	addr := getenvRequired("ADDR")

	// tolerans: PGPORT başında ":" varsa çıkar
	pgPort = strings.TrimPrefix(pgPort, ":")

	connStr := fmt.Sprintf("host=%s port=%s user=%s dbname=%s sslmode=disable", pgHost, pgPort, pgUser, pgDB)
	if pgPass != "" {
		connStr += " password=" + pgPass
	}

	var err error
	db, err = sql.Open("postgres", connStr) // assign to global db
	if err != nil {
		log.Fatal("sql.Open:", err)
	}

	// test ping
	ctx, cancel := withTimeoutCtx(2 * time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		log.Fatal("db ping:", err)
	}

	// router
	r := chi.NewRouter()
	r.Use(RequestLogger)

	r.Get("/items", listItems)
	r.Get("/items/{id}", getItem)
	r.With(AuthGuard).Post("/items", createItem)
	r.With(AuthGuard).Put("/items/{id}", updateItem)
	r.With(AuthGuard).Delete("/items/{id}", deleteItem)

	log.Printf("Server running on %s (connected to %s)\n", addr, connStr)
	if err := http.ListenAndServe(addr, r); err != nil {
		log.Fatal(err)
	}
}