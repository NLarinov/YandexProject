package main

// todo: another pages
// todo: agent

import (
	"encoding/json"
	"html/template"
	"log"
	"net/http"
	"os"
	"sync"
	"time"
)

type Operation struct {
	UserID      int       `json:"user_id"`
	OperationID int       `json:"operation_id"`
	Expression  string    `json:"expression"`
	Start       time.Time `json:"start"`
	Finish      time.Time `json:"finish"`
	Status      string    `json:"status"`
	result      int
	end         chan struct{}
}

type Cache struct {
	currData           map[int]Operation // key-OperationId value-Operation
	currentOperationID int
	locker             sync.RWMutex
	interval           time.Duration
	stop               chan struct{}
}

var GlobalCache Cache
var CurrentUserID int

func (o *Operation) agent() {
	// select {
	// case <-o.end:
	// 	o.result = parser
	// }
}

// todo: check expressions
func checkExpression(e string) bool {
	return true
}

func login(w http.ResponseWriter, r *http.Request) {
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
	GlobalCache.locker.RLock()
	defer GlobalCache.locker.RUnlock()
	for _, v := range GlobalCache.currData {
		if v.UserID == CurrentUserID && v.Expression == exp {
			return true
		}
	}
	return false
}

func addOperation(exp string) {
	GlobalCache.locker.Lock()
	defer GlobalCache.locker.Unlock()
	GlobalCache.currData[GlobalCache.currentOperationID] = Operation{
		UserID:      CurrentUserID,
		OperationID: GlobalCache.currentOperationID,
		Expression:  exp,
		Start:       time.Now(),
		Finish:      time.Now().Add(5 * time.Minute),
		Status:      "proceeding",
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
				log.Println("ivalid expression")
			}
		}
	}

	var fileName = "listExp.html"
	t, err := template.ParseFiles(fileName)
	if err != nil {
		log.Printf("Error while parsing the files: %s", err)
		return
	}
	err = t.ExecuteTemplate(w, fileName, GlobalCache.currData)
	if err != nil {
		log.Printf("Error while executing the files: %s", err)
		return
	}
}

// хендлер адресов
func handler(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/":
		login(w, r)
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

	var operations []Operation
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

func (c *Cache) updating() {
	ticker := time.NewTicker(c.interval)
	for {
		select {
		case <-ticker.C:
			c.update()
		case <-c.stop:
			ticker.Stop()
			return
		}
	}
}

func (c *Cache) update() {
	c.locker.Lock()
	defer c.locker.Unlock()
	for key, data := range c.currData {
		if data.Status == "proceeding" {
			if time.Now().After(data.Finish) {
				data.Status = "done"
				close(data.end)
				c.currData[key] = data
			} else {
				// запуск агента
				go data.agent()
			}
		}
	}
}

func NewCache(arr map[int]Operation, cleanUpInterval time.Duration) *Cache {
	cache := &Cache{
		currData: arr,
		interval: cleanUpInterval,
		stop:     make(chan struct{}),
	}

	if cleanUpInterval > 0 {
		go cache.updating()
	}
	return cache
}

func initialise(devMode bool, cleanUpInterval time.Duration) *Cache {
	var err error
	array := make(map[int]Operation)

	if devMode {
		array, err = readJSONFromFile("data.json")
		if err != nil {
			log.Fatal("error while reading json")
			return &Cache{}
		}
	}

	return NewCache(array, cleanUpInterval)
}

// функция запуска сервака
func main() {
	// параметры запуска, подробнее в README
	GlobalCache = *initialise(true, 30*time.Second)
	CurrentUserID = GlobalCache.currData[GlobalCache.currentOperationID].UserID + 1

	// todo: safe exit and stop updating

	http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))
	http.HandleFunc("/", handler)
	http.ListenAndServe("", nil)
}
