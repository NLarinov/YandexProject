package main

// todo: add fitch to change duration
// todo: save files after closing server and safe close
// todo: add comments and description

import (
	"encoding/json"
	"github.com/overseven/go-math-expression-parser/parser"
	"html/template"
	"log"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"
)

type Operation struct {
	UserID      int           `json:"user_id"`
	OperationID int           `json:"operation_id"`
	Expression  string        `json:"expression"`
	Start       time.Time     `json:"start"`
	Duration    time.Duration `json:"duration"`
	Status      string        `json:"status"`
	HeartBeat   time.Time     `json:"HeartBeat"`
	GoroutineId int           `json:"GoroutineId"`
	timer       time.Timer
}

type Cache struct {
	currData          map[int]Operation // ключ-OperationId значение-Operation
	lastOperationID   int
	locker            sync.RWMutex
	interval          time.Duration
	heartBeatDuration time.Duration
	stop              chan struct{}
}

var globalCache *Cache
var currentUserID int
var allowed = []rune{'1', '2', '3', '4', '5', '6', '7', '8', '9', '0', '+', '-', '*', '(', ')', ' '}
var flag bool

var devMode = true
var numberOfCalculatingServers = 5
var heartbeatDuration = time.Second * 10
var checkInterval = time.Second
var defaultTimerDuration = time.Second * 10

func (c *Cache) calculate(id int) {
	ticker := time.NewTicker(c.heartBeatDuration)
	for {
		select {
		case <-ticker.C:
			c.locker.Lock()
			val := c.currData[id]
			val.HeartBeat = time.Now()
			c.currData[id] = val
			c.locker.Unlock()
		case <-c.currData[id].timer.C:
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
			val.GoroutineId = -1

			c.locker.Lock()
			c.currData[id] = val
			c.locker.Unlock()

			log.Println("done", res, c.currData[id])
		}
	}
}

func (c *Cache) calculatingServer(i int) {
	ticker := time.NewTicker(c.interval)
	for {
		select {
		case <-ticker.C:
			c.locker.Lock()
			for id, value := range c.currData {
				if value.Status == "free" {
					value.Status = "proceeding"
					log.Println("proceed")
					value.GoroutineId = i
					c.currData[id] = value

					// по факту запускаем только одно вычислительную горутину ибо у нас и так есть вычислитель
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

func checkForRunning(exp string) bool {
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
	globalCache.currData[int(identificationNumber)] = Operation{
		UserID:      currentUserID,
		OperationID: int(identificationNumber),
		Expression:  exp,
		Start:       time.Now(),
		Duration:    time.Minute * 1,
		Status:      "free",
		timer:       *time.NewTimer(defaultTimerDuration),
	}
}

// хендлер страницы с выражениями
func listExp(w http.ResponseWriter, r *http.Request) {

	if r.Method == "POST" {
		expression := r.FormValue("expression")
		log.Println(expression)

		ok := checkForRunning(expression)

		if ok {
			w.WriteHeader(http.StatusOK)
			log.Println("already in progress")
		} else {
			if checkExpression(expression) {
				w.WriteHeader(http.StatusOK)
				log.Println("valid expression")

				addOperation(expression)
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
	err = t.ExecuteTemplate(w, fileName, globalCache.currData)
	if err != nil {
		log.Printf("Error while executing the files: %s", err)
		return
	}
}

// хендлер адресов
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

// функция записи в файл JSON
func writeJSONToFile(filename string, operations map[int]Operation) error {
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

// функция чтения из файла JSON
func readJSONFromFile(filename string) (map[int]Operation, error) {
	operationsJSON, err := os.ReadFile(filename)
	if err != nil {
		log.Println(err)
		return nil, err
	}

	var operations map[int]Operation
	err = json.Unmarshal(operationsJSON, &operations)
	if err != nil {
		return nil, err
	}

	opr := make(map[int]Operation)

	for _, v := range operations {
		opr[v.OperationID] = v
	}

	return opr, nil
}

//func (c *Cache) updating() {
//	ticker := time.NewTicker(c.interval)
//	for {
//		select {
//		case <-ticker.C:
//			c.update()
//		case <-c.stop:
//			ticker.Stop()
//			return
//		}
//	}
//}
//
//func (c *Cache) update() {
//	//c.locker.Lock()
//	//defer c.locker.Unlock()
//	//for _, data := range c.currData {
//	//	if data.Status == "proceeding" {
//	//		if time.Now().After(data.Finish) {
//	//			data.Status = "done"
//	//			close(data.end)
//	//		}
//	//	}
//	//}
//}

func demon(arr map[int]Operation) *Cache {
	cache := &Cache{
		currData:          arr,
		interval:          checkInterval,
		heartBeatDuration: heartbeatDuration,
		stop:              make(chan struct{}),
	}

	for i := 0; i < numberOfCalculatingServers; i++ {
		// запускаем вычислительные серваки
		go cache.calculatingServer(i)
	}

	return cache
}

func initialise() *Cache {
	var err error
	array := make(map[int]Operation)

	if devMode {
		array, err = readJSONFromFile("data.json")
		if err != nil {
			log.Println(err)
			log.Fatal("error while reading json")
			return &Cache{}
		}
	}

	return demon(array)
}

// функция запуска сервака
func main() {
	// параметры запуска, подробнее в README
	globalCache = initialise()
	currentUserID = globalCache.currData[globalCache.lastOperationID].UserID + 1

	// todo: safe exit and stop updating

	http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))
	http.HandleFunc("/", handler)
	err := http.ListenAndServe("", nil)
	if err != nil {
		log.Fatal("server error")
		return
	}
}
