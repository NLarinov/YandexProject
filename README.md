# YandexCalc

_This is my Yandex golang project_

### Init:

_to run server:_ 
```
go env -w CGO_ENABLED=1
go run main.go
```

you have to wait a minute for DB to init

code has all requireable comments about functions and structures

### Steps:

* Open main page http://localhost:8000
* Open openapi.yml with Swagger or Redoc(preferable)
* Register
* Login, get a token
* Enter an expression and the token
* Enjoy)

to add time (addDuration) click on "ADD TIME" on listExp.html;
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

#### Contacts:
you can write me here https://t.me/n_larinovvv
