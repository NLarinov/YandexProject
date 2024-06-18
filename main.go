package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/golang-jwt/jwt/v5"
	_ "github.com/mattn/go-sqlite3"
	"github.com/overseven/go-math-expression-parser/parser"
	"golang.org/x/sync/errgroup"
)

// Operation structure for operation
type Operation struct {
	UserID      int
	OperationID string // unique number for each operation
	Expression  string // mathematical expression
	Start       string // start time of operation
	Duration    string // duration of operation
	Status      string // status of operation (done, proceeding, free)
	HeartBeat   string // period of sending info from goroutine
	GoroutineId int
	timer       chan interface{} // close channel
}

// User structure for reading from db
type User struct {
	Id       int
	Login    string
	Password string
	Token    string
}

// Cache structure for cache, which includes all current running operations
type Cache struct {
	currData          map[string]Operation // key OperationId, value Operation
	interval          time.Duration        // refresh duration for calculatingServer
	heartBeatDuration time.Duration        // refresh duration for calculate
	stop              chan struct{}        // close channel
	locker            sync.RWMutex
}

// do not change
var globalCache *Cache
var db *sql.DB
var currentUserID int
var allowed = []rune{'1', '2', '3', '4', '5', '6', '7', '8', '9', '0', '+', '-', '*', '/', '(', ')', ' '}
var flag bool

// const variables (changeable)
var numberOfCalculatingServers = 5          // number of servers that are active after server starts
var heartbeatDuration = time.Second * 2     // (Cache.heartBeatDuration)
var checkInterval = time.Second             // (Cache.interval)
var defaultTimerDuration = time.Second * 10 // (Operation.Duration)
var addDuration = time.Second * 10          // duration that adds after clicking "ADD TIME" on site

// goroutine for calculating expressions
func (c *Cache) calculate(id string) {
	ticker := time.NewTicker(c.heartBeatDuration)
	for {
		select {
		case <-ticker.C: // refresh duration and send heartbeat
			c.locker.Lock()
			val := c.currData[id]
			val.HeartBeat = time.Now().String()
			times, _ := time.ParseDuration(val.Duration)
			val.Duration = (times - heartbeatDuration).String()
			if times <= 0 {
				close(val.timer)
			}
			c.currData[id] = val
			c.locker.Unlock()
		case <-c.currData[id].timer: // evaluate expression and end calculation
			c.locker.RLock()
			val := c.currData[id]
			c.locker.RUnlock()

			prs := parser.NewParser()
			_, err := prs.Parse(val.Expression)
			if err != nil {
				val.Status = "invalid expression"
				return
			}
			res, err := prs.Evaluate(make(map[string]float64))
			if err != nil {
				val.Status = "invalid expression"
				return
			}
			result := strconv.FormatFloat(res, 'G', -1, 32)
			if val.Expression[len(val.Expression)-len(result):] != result {
				val.Expression += " = " + result
			}

			val.Status = "done"
			val.Duration = (time.Second * 0).String()
			val.GoroutineId = -1
			ticker.Stop()

			c.locker.Lock()
			c.currData[id] = val
			c.locker.Unlock()
		}
	}
}

// server for calculations
func (c *Cache) calculatingServer(i int) {
	ticker := time.NewTicker(c.interval)
	for {
		select {
		case <-ticker.C: // refresh and find free expressions
			c.locker.Lock()
			for id, value := range c.currData {
				if value.Status == "free" {
					value.Status = "proceeding"
					log.Println("proceeded")
					value.GoroutineId = i
					c.currData[id] = value

					// deploy calculating goroutine
					go c.calculate(id)

				}
			}
			c.locker.Unlock()
		case <-c.stop:
			ticker.Stop()
			return
		}
	}
}

// check if expression is valid
func checkExpression(e string) bool {
	for _, v1 := range e {
		flag = false
		for _, v2 := range allowed {
			if v1 == v2 {
				flag = true
				break
			}
		}
		if !flag {
			return false
		}
	}
	return true
}

// handle default page
func handleAddExpression(w http.ResponseWriter) {
	var fileName = "templates/new.html"
	t, err := template.ParseFiles(fileName)
	if err != nil {
		log.Printf("Error while parsing the files: %s", err)
		return
	}
	err = t.ExecuteTemplate(w, "new.html", nil)
	if err != nil {
		log.Printf("Error while executing the files: %s", err)
		return
	}
}

// check if expression is already running
func checkForRunning(exp string) bool {
	if len(exp) == 0 {
		return true
	}

	globalCache.locker.RLock()
	defer globalCache.locker.RUnlock()
	for _, v := range globalCache.currData {
		if v.UserID == currentUserID && v.Expression == exp {
			return true
		}
	}
	return false
}

func addOperation(exp string) {
	identificationNumber := time.Now().Unix()
	globalCache.locker.Lock()
	defer globalCache.locker.Unlock()
	globalCache.currData[strconv.Itoa(int(identificationNumber))] = Operation{
		UserID:      currentUserID,
		OperationID: strconv.Itoa(int(identificationNumber)),
		Expression:  exp,
		Start:       time.Now().String(),
		Duration:    defaultTimerDuration.String(),
		Status:      "free",
		timer:       make(chan interface{}),
	}
}

// add time to Operation.Duration
func addTime(id string) {
	globalCache.locker.Lock()
	defer globalCache.locker.Unlock()

	val := globalCache.currData[id]
	times, _ := time.ParseDuration(val.Duration)
	val.Duration = (times + addDuration).String()

	globalCache.currData[id] = val
}

// handle page with expressions
func listExp(w http.ResponseWriter, r *http.Request) {
	if r.Method == "POST" {
		id := r.FormValue("id")
		if len(id) != 0 {
			addTime(id)
		} else {
			tokenString := r.FormValue("token")
			if tokenString == "" {
				w.WriteHeader(http.StatusBadRequest)
				return
			}

			token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
				return []byte("your-secret-key-here"), nil
			})
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				log.Printf("Error while parsing the token: %s", err)
				return
			}
			claims, ok := token.Claims.(jwt.MapClaims)
			if !ok || !token.Valid {
				w.WriteHeader(http.StatusBadRequest)
				log.Println(err)
				return
			}
			userLogin, ok := claims["login"].(string)
			if !ok {
				w.WriteHeader(http.StatusBadRequest)
				log.Println(err)
				return
			}

			var userID int
			err = db.QueryRow(`SELECT id FROM users WHERE login=$1`, userLogin).Scan(&userID)
			if err != nil {
				log.Printf("Error while quering the db: %s", err)
			}

			if userID == 0 {
				w.WriteHeader(http.StatusUnauthorized)
			}

			currentUserID = userID
			expression := r.FormValue("expression")

			ok = checkForRunning(expression) // check if expression is already running

			if ok {
				w.WriteHeader(http.StatusOK)
				log.Println("already in progress")
			} else {
				if checkExpression(expression) { // if expression is valid
					w.WriteHeader(http.StatusOK)
					log.Println("valid expression")

					addOperation(expression) // adding operation to the globalCache
				} else {
					w.WriteHeader(http.StatusBadRequest)
					log.Println("invalid expression")
				}
			}
		}
	} else {
		idUser := r.FormValue("button")
		if len(idUser) != 0 {
			currentUserID, _ = strconv.Atoi(idUser)
		} else {
			currentUserID = -1
		}
	}

	res := make(map[string]Operation)

	for s, v := range globalCache.currData {
		if v.UserID == currentUserID {
			res[s] = v
		}
	}

	var fileName = "templates/listExp.html"
	t, err := template.ParseFiles(fileName)
	if err != nil {
		log.Printf("Error while parsing the files: %s", err)
		return
	}
	err = t.ExecuteTemplate(w, "listExp.html", res) // executing page with data from Cache
	if err != nil {
		log.Printf("Error while executing the files: %s", err)
		return
	}
}

func register(w http.ResponseWriter, r *http.Request) {
	var user User
	err := json.NewDecoder(r.Body).Decode(&user)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	var count int
	err = db.QueryRow(`SELECT COUNT(*) FROM users WHERE login=$1`, user.Login).Scan(&count)
	if err != nil {
		log.Fatal(err)
	}

	if count == 1 {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	_, err = db.Exec(`REPLACE INTO users (id, login, password) VALUES ($1, $2, $3)`, strconv.Itoa(int(time.Now().Unix())), user.Login, user.Password)
	if err != nil {
		log.Fatal(err)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
}

func login(w http.ResponseWriter, r *http.Request) {
	var mainUser User
	err := json.NewDecoder(r.Body).Decode(&mainUser)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	var count int
	err = db.QueryRow(`SELECT COUNT(*) FROM users WHERE login=$1`, mainUser.Login).Scan(&count)
	if err != nil {
		log.Fatal(err)
	}

	if count == 0 {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	token := jwt.New(jwt.SigningMethodHS256)
	claims := token.Claims.(jwt.MapClaims)
	claims["login"] = mainUser.Login
	claims["exp"] = time.Now().Add(24 * time.Hour).Unix()

	tokenString, err := token.SignedString([]byte("your-secret-key-here"))
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(tokenString))
}

// page handlers
func handler(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/":
		handleAddExpression(w)
	case "/list-ex":
		listExp(w, r)
	case "/api/v1/register":
		register(w, r)
	case "/api/v1/login":
		login(w, r)
	default:
		http.Error(w, "404 PAGE NOT FOUND", http.StatusNotFound)
	}
}

// write to file
func writeSQL(operations map[string]Operation) error {
	for _, v := range operations {
		_, err := db.Exec(`REPLACE INTO operations (user_id, operation_id, expression, start, duration, status, heartbeat, goroutine_id) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
			v.UserID, v.OperationID, v.Expression, v.Start, v.Duration, v.Status, v.HeartBeat, v.GoroutineId)
		if err != nil {
			return err
		}
	}

	return nil
}

// read from db
func readSQL() (map[string]Operation, error) {
	rows, err := db.Query(`SELECT * from operations`)
	if err != nil {
		log.Fatal(err)
	}

	operations := make(map[string]Operation)

	for rows.Next() {
		var op Operation
		err = rows.Scan(&op.UserID, &op.OperationID, &op.Expression, &op.Start, &op.Duration, &op.Status, &op.HeartBeat, &op.GoroutineId)
		if err != nil {
			log.Fatal(err)
			return nil, err
		}

		// if server was closed before ending operation
		if op.Status == "proceeding" {
			op.Status = "free"
		}
		operations[op.OperationID] = op
	}

	return operations, nil
}

// Establish SQL database connection
func establishSQLConnection(arr map[string]Operation) *Cache {
	cache := &Cache{
		currData:          arr,
		interval:          checkInterval,
		heartBeatDuration: heartbeatDuration,
		stop:              make(chan struct{}),
	}

	for i := 0; i < numberOfCalculatingServers; i++ {
		// deploying calculating servers
		go cache.calculatingServer(i)
	}

	return cache
}

// initialize Cache
func initialize() *Cache {
	var err error

	array, err := readSQL()
	if err != nil {
		log.Println(err)
		log.Fatal("error while reading from SQL database")
		return &Cache{}
	}

	return establishSQLConnection(array)
}

// deploying server
func main() {
	// deploying server and graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		c := make(chan os.Signal, 1) // we need to reserve to buffer size 1, so the notifier are not blocked
		signal.Notify(c, os.Interrupt, syscall.SIGTERM)

		<-c
		cancel()
	}()

	log.Println("starting db...")

	var err error
	db, err = sql.Open("sqlite3", "db/calc.db")
	if err != nil {
		log.Fatal(err)
		return
	}

	if err = db.Ping(); err != nil {
		log.Fatal(err)
	}

	globalCache = initialize()
	log.Println("starting server...")

	http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))
	http.HandleFunc("/", handler)

	httpServer := &http.Server{
		Addr: ":8000",
	}

	g, gCtx := errgroup.WithContext(ctx)

	g.Go(func() error {
		return httpServer.ListenAndServe()
	})

	log.Println("Server started on port 8000")

	g.Go(func() error {
		<-gCtx.Done()

		close(globalCache.stop)
		err = writeSQL(globalCache.currData)
		if err != nil {
			log.Println("error while writing to db")
		}
		_ = db.Close()

		return httpServer.Shutdown(context.Background())
	})

	if err = g.Wait(); err != nil {
		fmt.Printf("exit reason: %s \n", err)
	}
}
