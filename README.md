# YandexCalc

_Hello! This is my Yandex project on Golang educational course. The technical task was to make Go application using MySQL, JWT, API and goroutines._

### Structure:
- A server that accepts an arithmetic expression, translates it into a set of sequential tasks and ensures the order of their execution - the "orchestrator"
 
- A computer that can receive a task from the "orchestrator", execute it and return the result to the server - the "agent"

### Init:

_to run server:_ 
```
go run main.go
```

you have to wait a minute for DB to init

code has all requireable comments about functions and structures

### Steps:

* Open main page http://localhost:8000
* Open openapi.yml with Swagger or Redoc(preferable)
* Register with api
* Login, get a token with api
* Enter an expression and the token on the main page
* Enjoy)

to add time (addDuration) click the button "ADD TIME" on listExp page;
list of expressions shows all required data (working goroutines, id's, e.t.c);
to see updating data you have to refresh page (or click on button refresh)

### Commands:
input on main page:
```
1+2+3
```
```
1*2+4*10*12345312
```
```
1+2+3/5*(10+9)/3
```

#### P.S.
if you kill one calculating server, it's operation status will stay
"proceeding", so you need to restart server to rerun expression
