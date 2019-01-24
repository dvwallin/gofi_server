# gofi_server
GoFi File Sync Server

## install
```cd $GOPATH```
```go install github.com/dvwallin/gofi_server```

## usage
```cd <path-you-want-your-file-database-in>```
```gofi_server```

## about
Just an UDP server that accepts data from gofi_client and writes data to an sqlite3 database. The GoFi project is intended as a form of file-indexer for many machines in a network.

## todo
* Add a web UI to view all indexed files and sort on size and search
* Figure out concurrency
