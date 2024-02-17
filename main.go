package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/overseven/go-math-expression-parser/parser"
	"golang.org/x/sync/errgroup"
	"html/template"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"
)

// Operation structure for operation
type Operation struct {
	UserID      int              `json:"user_id"`
	OperationID string           `json:"operation_id"` // unique number for each operation
	Expression  string           `json:"expression"`   // mathematical expression
	Start       time.Time        `json:"start"`        // start time of operation
	Duration    time.Duration    `json:"duration"`     // duration of operation
	Status      string           `json:"status"`       // status of operation (done, proceeding, free)
	HeartBeat   time.Time        `json:"HeartBeat"`    // period of sending info from goroutine
	GoroutineId int              `json:"GoroutineId"`
	timer       chan interface{} // close channel
}

// Cache structure for cache, which includes all current running operations
type Cache struct {
	currData          map[string]Operation // key OperationId, value Operation
	lastOperationID   int                  // id of last registered Operation
	interval          time.Duration        // refresh duration for calculatingServer
	heartBeatDuration time.Duration        // refresh duration for calculate
	stop              chan struct{}        // close channel
	locker            sync.RWMutex
}

// do not change
var globalCache *Cache
var currentUserID int
var allowed = []rune{'1', '2', '3', '4', '5', '6', '7', '8', '9', '0', '+', '-', '*', '/', '(', ')', ' '}
var flag bool

// const variables (changeable)
var devMode = true                          // if on: loads data from database in Cache, else makes empty Cache
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
			val.HeartBeat = time.Now()
			val.Duration -= c.heartBeatDuration
			if val.Duration <= 0 {
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
			val.Expression += " = " + strconv.FormatFloat(res, 'G', -1, 32)

			val.Status = "done"
			val.Duration = 0
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
func login(w http.ResponseWriter) {
	var fileName = "new.html"
	t, err := template.ParseFiles(fileName)
	if err != nil {
		log.Printf("Error while parsing the files: %s", err)
		return
	}
	err = t.ExecuteTemplate(w, fileName, nil)
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
		Start:       time.Now(),
		Duration:    defaultTimerDuration,
		Status:      "free",
		timer:       make(chan interface{}),
	}
}

// add time to Operation.Duration
func addTime(id string) {
	globalCache.locker.Lock()
	defer globalCache.locker.Unlock()

	val := globalCache.currData[id]
	val.Duration += addDuration

	globalCache.currData[id] = val
}

// handle page with expressions
func listExp(w http.ResponseWriter, r *http.Request) {
	if r.Method == "POST" {
		expression := r.FormValue("expression")
		id := r.FormValue("id")

		if len(expression) == 0 {
			addTime(id)
		}

		ok := checkForRunning(expression) // check if expression is already running

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

	var fileName = "listExp.html"
	t, err := template.ParseFiles(fileName)
	if err != nil {
		log.Printf("Error while parsing the files: %s", err)
		return
	}
	err = t.ExecuteTemplate(w, fileName, globalCache.currData) // executing page with data from Cache
	if err != nil {
		log.Printf("Error while executing the files: %s", err)
		return
	}
}

// page handlers
func handler(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/":
		login(w)
	case "/list-ex":
		listExp(w, r)
	default:
		http.Error(w, "404 PAGE NOT FOUND", http.StatusNotFound)
	}
}

// write to file
func writeJSONToFile(filename string, operations map[string]Operation) error {
	operationsJSON, err := json.Marshal(operations)
	if err != nil {
		return err
	}

	err = os.WriteFile(filename, operationsJSON, 0644)
	if err != nil {
		return err
	}

	return nil
}

// read to JSON
func readJSONFromFile(filename string) (map[string]Operation, error) {
	operationsJSON, err := os.ReadFile(filename)
	if err != nil {
		log.Println(err)
		return nil, err
	}

	var operations map[string]Operation
	err = json.Unmarshal(operationsJSON, &operations)
	if err != nil {
		return nil, err
	}

	opr := make(map[string]Operation)

	for _, v := range operations {
		if v.Status == "proceeding" { // if server was closed before ending operation
			v.Status = "free"
		}
		v.timer = make(chan interface{})
		opr[v.OperationID] = v
	}

	return opr, nil
}

func demon(arr map[string]Operation) *Cache {
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

// initialise Cache
func initialise() *Cache {
	var err error
	array := make(map[string]Operation)

	if devMode {
		array, err = readJSONFromFile("db/data.json")
		if err != nil {
			log.Println(err)
			log.Fatal("error while reading json")
			return &Cache{}
		}
	}

	return demon(array)
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

	globalCache = initialise()
	currentUserID = globalCache.currData[strconv.Itoa(globalCache.lastOperationID)].UserID + 1 // increment userID

	http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))
	http.HandleFunc("/", handler)

	httpServer := &http.Server{
		Addr: ":8000",
	}

	g, gCtx := errgroup.WithContext(ctx)

	g.Go(func() error {
		return httpServer.ListenAndServe()
	})

	g.Go(func() error {
		<-gCtx.Done()

		close(globalCache.stop)                                      // close Cache
		err := writeJSONToFile("db/data.json", globalCache.currData) // write Cache to file
		if err != nil {
			log.Println("error while writing to file")
		}

		return httpServer.Shutdown(context.Background())
	})

	if err := g.Wait(); err != nil {
		fmt.Printf("exit reason: %s \n", err)
	}
}
