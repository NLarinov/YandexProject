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
	"syscall"
	"time"

	"github.com/golang-jwt/jwt/v5"
	_ "github.com/mattn/go-sqlite3"
	"github.com/overseven/go-math-expression-parser/parser"
	"golang.org/x/sync/errgroup"
)

// current user id
var currentUserID int

// allowed operators
var allowed = []rune{'1', '2', '3', '4', '5', '6', '7', '8', '9', '0', '+', '-', '*', '/', '(', ')', ' '}

// goroutine for calculating expressions
func (c *Cache) calculate(id string) {
	ticker := time.NewTicker(c.heartBeatDuration)
	for {
		select {
		case <-ticker.C: // refresh duration and send heartbeat
			c.locker.Lock()
			val := c.operData[id]
			val.HeartBeat = time.Now().String()
			times, _ := time.ParseDuration(val.Duration)
			val.Duration = (times - heartbeatDuration).String()
			if times <= 0 {
				close(val.timer)
			}
			c.operData[id] = val
			c.locker.Unlock()
		case <-c.operData[id].timer: // evaluate expression and end calculation
			c.locker.RLock()
			val := c.operData[id]
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
			c.operData[id] = val
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
			for id, value := range c.operData {
				if value.Status == "free" {
					value.Status = "proceeding"
					log.Println("proceeded")
					value.GoroutineId = i
					c.operData[id] = value

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
	var flag bool
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

// handle error page
func handleErr(w http.ResponseWriter) {
	var fileName = "templates/errPage.html"
	t, err := template.ParseFiles(fileName)
	if err != nil {
		log.Printf("Error while parsing the files: %s", err)
		return
	}
	err = t.ExecuteTemplate(w, "errPage.html", nil)
	if err != nil {
		log.Printf("Error while executing the files: %s", err)
		return
	}
}

// check if expression is already running
func (c *Cache) checkForRunning(exp string) bool {
	if len(exp) == 0 {
		return true
	}

	c.locker.RLock()
	defer c.locker.RUnlock()
	for _, v := range c.operData {
		if v.UserID == currentUserID && v.Expression == exp {
			return true
		}
	}
	return false
}

func (c *Cache) addOperation(exp string) {
	identificationNumber := time.Now().Unix()
	c.locker.Lock()
	defer c.locker.Unlock()
	c.operData[strconv.Itoa(int(identificationNumber))] = Operation{
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
func (c *Cache) addTime(id string) {
	c.locker.Lock()
	defer c.locker.Unlock()

	val := c.operData[id]
	times, _ := time.ParseDuration(val.Duration)
	val.Duration = (times + addDuration).String()

	c.operData[id] = val
}

// handle page with expressions
func (c *Cache) listExp(w http.ResponseWriter, r *http.Request) {
	if r.Method == "POST" {
		id := r.FormValue("id")
		if len(id) != 0 {
			c.addTime(id)
		} else {
			tokenString := r.FormValue("token")
			if tokenString == "" {
				w.WriteHeader(http.StatusBadRequest)
				handleErr(w)
				return
			}

			token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
				return []byte("your-secret-key-here"), nil
			})
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				log.Printf("Error while parsing the token: %s", err)
				handleErr(w)
				return
			}
			claims, ok := token.Claims.(jwt.MapClaims)
			if !ok || !token.Valid {
				w.WriteHeader(http.StatusBadRequest)
				log.Println(err)
				handleErr(w)
				return
			}
			userLogin, ok := claims["login"].(string)
			if !ok {
				w.WriteHeader(http.StatusBadRequest)
				log.Println(err)
				handleErr(w)
				return
			}

			v, ok := c.userData[userLogin]

			if !ok {
				w.WriteHeader(http.StatusUnauthorized)
			}

			currentUserID = v.Id
			expression := r.FormValue("expression")

			ok = c.checkForRunning(expression) // check if expression is already running

			if ok {
				w.WriteHeader(http.StatusOK)
				log.Println("already in progress")
			} else {
				if checkExpression(expression) { // if expression is valid
					w.WriteHeader(http.StatusOK)
					log.Println("valid expression")

					c.addOperation(expression) // adding operation to the globalCache
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

	for s, v := range c.operData {
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

func (c *Cache) register(w http.ResponseWriter, r *http.Request) {
	var user User
	err := json.NewDecoder(r.Body).Decode(&user)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	if _, ok := c.userData[user.Login]; ok {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	c.userData[user.Login] = User{Id: int(time.Now().Unix()), Login: user.Login, Password: user.Password}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
}

func (c *Cache) login(w http.ResponseWriter, r *http.Request) {
	var mainUser User
	err := json.NewDecoder(r.Body).Decode(&mainUser)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	if _, ok := c.userData[mainUser.Login]; !ok {
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
func (c *Cache) handler(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/":
		handleAddExpression(w)
	case "/list-ex":
		c.listExp(w, r)
	case "/api/v1/register":
		c.register(w, r)
	case "/api/v1/login":
		c.login(w, r)
	default:
		http.Error(w, "404 PAGE NOT FOUND", http.StatusNotFound)
	}
}

// write to file
func writeSQL(operations map[string]Operation, users map[string]User, db *sql.DB) error {
	for _, v := range operations {
		_, err := db.Exec(`REPLACE INTO operations (user_id, operation_id, expression, start, duration, status, heartbeat, goroutine_id) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
			v.UserID, v.OperationID, v.Expression, v.Start, v.Duration, v.Status, v.HeartBeat, v.GoroutineId)
		if err != nil {
			return err
		}
	}

	for _, v := range users {
		_, err := db.Exec(`REPLACE INTO users (id, login, password) VALUES ($1, $2, $3)`,
			strconv.Itoa(v.Id), v.Login, v.Password)
		if err != nil {
			return err
		}
	}

	return nil
}

func readUsers(db *sql.DB) (map[string]User, error) {
	rows, err := db.Query(`SELECT * from operations`)
	if err != nil {
		log.Fatal(err)
	}

	users := make(map[string]User)

	for rows.Next() {
		var u User
		err = rows.Scan(&u.Id, &u.Login, &u.Password)
		if err != nil {
			log.Fatal(err)
			return nil, err
		}
		users[u.Login] = u
	}

	return users, nil
}

// read users from db
func readOperations(db *sql.DB) (map[string]Operation, error) {
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
func establishSQLConnection(operations map[string]Operation, users map[string]User) *Cache {
	cache := &Cache{
		userData:          users,
		operData:          operations,
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
func initialize(db *sql.DB) *Cache {
	var err error

	operations, err := readOperations(db)
	if err != nil {
		log.Println(err)
		log.Fatal("error while reading from SQL database")
		return &Cache{}
	}

	users, err := readUsers(db)
	if err != nil {
		log.Println(err)
		log.Fatal("error while reading from SQL database")
		return &Cache{}
	}

	return establishSQLConnection(operations, users)
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

	db, err := sql.Open("sqlite3", "db/calc.db")
	if err != nil {
		log.Fatal(err)
		return
	}

	if err = db.Ping(); err != nil {
		log.Fatal(err)
	}

	globalCache := initialize(db)
	log.Println("starting server...")

	http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))
	http.HandleFunc("/", globalCache.handler)

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
		err = writeSQL(globalCache.operData, globalCache.userData, db)
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
