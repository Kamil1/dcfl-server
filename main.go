package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"

	"github.com/gorilla/mux"
	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
	"github.com/rs/cors"
	"github.com/rubenv/sql-migrate"
)

const devDbHost = "DCFL_DEV_PG_HOST"
const devDbPort = "DCFL_DEV_PG_PORT"
const devDbUser = "DCFL_DEV_PG_USER"
const devDbName = "DCFL_DEV_PG_NAME"
const devDbURL = "DATABASE_URL"
const devDbDriver = "postgres"
const migrationsDirectory = "migrations/postgres"
const tokeninfoEndpoint = "https://www.googleapis.com/oauth2/v3/tokeninfo?id_token="

var db *sql.DB

type validatedID struct {
	Iss        string `json:"iss"`
	Sub        string `json:"sub"`
	Azp        string `json:"azp"`
	Aud        string `json:"aud"`
	Iat        string `json:"iat"`
	Exp        string `json:"exp"`
	Email      string `json:"email"`
	Name       string `json:"name"`
	Picture    string `json:"picture"`
	GivenName  string `json:"given_name"`
	FamilyName string `json:"family_name"`
}

type Request struct {
	w http.ResponseWriter
	r *http.Request
}

func IndexHandler(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte("Hello World!"))
}

func AuthenticateHandler(w http.ResponseWriter, r *http.Request) {
	resp, err := http.Get(tokeninfoEndpoint + r.Header.Get("Authorization"))
	if err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	token := &validatedID{}
	err = json.Unmarshal(body, token)
	if err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	if *token == (validatedID{}) {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM public.player WHERE id = $1", token.Sub).Scan(&count)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	if count == 0 {
		_, err := db.Query(
			"INSERT INTO public.player(id, name, picture) VALUES ($1, $2, $3)",
			token.Sub,
			token.Name,
			token.Picture)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(token)
}

func initDB() {
	var dbInfo string
	if env := os.Getenv("ENV"); env == "DEV" {
		err := godotenv.Load()
		if err != nil {
			log.Fatal("Error loading .env file")
		}

		dbInfo = fmt.Sprintf("host=%s port=%s user=%s "+"dbname=%s sslmode=disable",
			os.Getenv(devDbHost),
			os.Getenv(devDbPort),
			os.Getenv(devDbUser),
			os.Getenv(devDbName),
		)

	} else if env == "PROD" {
		dbInfo = os.Getenv(devDbURL)
	} else {
		log.Fatal("$ENV must be set")
	}

	var err error
	db, err = sql.Open(devDbDriver, dbInfo)
	if err != nil {
		log.Fatalf("Error opening database: %q", err)
	}

	migrations := &migrate.FileMigrationSource{
		Dir: migrationsDirectory,
	}

	applies, err := migrate.Exec(db, devDbDriver, migrations, migrate.Up)
	if err != nil {
		log.Fatalf("Exec: %v", err)
	}

	fmt.Printf("Applied %d migrations!\n", applies)
}

func main() {
	initDB()
	defer db.Close()

	h := newHub()
	router := mux.NewRouter()
	router.HandleFunc("/", IndexHandler).Methods("GET")
	router.HandleFunc("/authenticate", AuthenticateHandler).Methods("POST")
	router.Handle("/register/{sub:[0-9]+}", wsHandler{h: h})

	handler := cors.New(cors.Options{
		AllowedHeaders: []string{"*"},
	}).Handler(router)

	log.Fatal(http.ListenAndServe(fmt.Sprint(":", os.Getenv("PORT")), handler))
}
