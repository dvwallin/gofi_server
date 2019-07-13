package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
	_ "github.com/mattn/go-sqlite3"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"gopkg.in/cheggaaa/pb.v1"
)

const (
	BUFFERSIZE         = 2048
	SERVER_PORT        = 1985
	GOFI_DATABASE_NAME = "gofi.db"
)

var (
	db        *sql.DB
	mdb       *mongo.Client
	err       error
	stmt      *sql.Stmt
	res       sql.Result
	fileCount int = 0
)

type (
	File struct {
		ID               string `json:"id,omitempty"`
		Name             string `json:"name,omitempty"`
		Path             string `json:"path,omitempty"`
		Size             int64  `json:"size"`
		IsDir            int    `json:"isdir"`
		Machine          string `json:"machine"`
		IP               string `json:"ip"`
		OnExternalSource int    `json:"on_external_source"`
		ExternalName     string `json:"external_name"`
		FileType         string `json:"file_type"`
		FileMIME         string `json:"file_mime"`
		SHA512           string `json:"sha512"` // TODO : ADD SHA512 HASH FOR EACH FILE!!!!
	}
	Files []File
)

func init() {

	// Connect to the database
	db, err = sql.Open("sqlite3", fmt.Sprintf("./%s", GOFI_DATABASE_NAME))
	if err != nil {
		log.Println(err)
	}

	ctx, _ := context.WithTimeout(context.Background(), 10*time.Second)
	mdb, err = mongo.Connect(ctx, options.Client().ApplyURI("mongodb://localhost:27017"))

}

func main() {
	server, err := net.Listen("tcp", fmt.Sprintf(":%d", SERVER_PORT))
	if err != nil {
		log.Println("error listetning: ", err)
		os.Exit(1)
	}
	defer server.Close()
	go www()
	log.Printf("Server started on :%d! Waiting for connections...\n", SERVER_PORT)
	for {
		connection, err := server.Accept()
		if err != nil {
			log.Println("error: ", err)
			os.Exit(1)
		}
		log.Println("client connected from", connection.RemoteAddr().String())
		filename, err := getFile(connection)
		if err != nil {
			log.Println("error getting file", err)
		}
		err = addFile(filename)
		if err != nil {
			log.Println("error adding file", filename, err)
		} else {
			log.Println("files successfully added ...")
		}

		log.Println("transaction ended ...")

	}
}

func www() {
	r := mux.NewRouter()
	r.HandleFunc("/", ListFiles)
	r.HandleFunc("/test", Test)
	log.Println("WWW running on port 1180 ...")
	http.ListenAndServe(":1180", r)
}

func Test(w http.ResponseWriter, req *http.Request) {
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "HEJ!")
}
func getFile(connection net.Conn) (filename string, err error) {
	bufferFileName := make([]byte, 64)
	bufferFileSize := make([]byte, 10)

	connection.Read(bufferFileSize)
	fileSize, _ := strconv.ParseInt(strings.Trim(string(bufferFileSize), ":"), 10, 64)

	connection.Read(bufferFileName)
	filename = strings.Trim(string(bufferFileName), ":")

	newFile, err := os.Create(filename)
	if err != nil {
		return filename, err
	}

	defer newFile.Close()
	var receivedBytes int64

	for {
		if (fileSize - receivedBytes) < BUFFERSIZE {
			io.CopyN(newFile, connection, (fileSize - receivedBytes))
			connection.Read(make([]byte, (receivedBytes+BUFFERSIZE)-fileSize))
			break
		}
		io.CopyN(newFile, connection, BUFFERSIZE)
		receivedBytes += BUFFERSIZE
	}
	log.Println("successfully received", filename)
	log.Println("file is", ByteCountSI(receivedBytes), "bytes")
	return filename, nil
}

func addFile(filename string) (err error) {

	log.Println("initiating saving files to database ...")

	var files Files

	tempDB, err := sql.Open("sqlite3", filename)
	if err != nil {
		return err
	}

	rows, err := tempDB.Query("select name, path, size, isdir, machine, ip, onexternalsource, externalname, filetype, filemime from files")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var file File
		err = rows.Scan(&file.Name, &file.Path, &file.Size, &file.IsDir, &file.Machine, &file.IP, &file.OnExternalSource, &file.ExternalName, &file.FileType, &file.FileMIME)
		if err != nil {
			return err
		}
		files = append(files, file)
	}
	err = rows.Err()
	if err != nil {
		return err
	}

	count := len(files)

	log.Println("found", count, "files in", filename)

	bar := pb.StartNew(count)

	tx, err := db.Begin()
	if err != nil {
		return err
	}

	for _, v := range files {
		bar.Increment()

		stmt, err = tx.Prepare("INSERT OR IGNORE INTO files(name, path, size, isdir, machine, ip, onexternalsource, externalname, filetype, filemime) values(?,?,?,?,?,?,?,?,?,?)")

		if err != nil {
			return err
		}

		_, err = stmt.Exec(v.Name, v.Path, v.Size, v.IsDir, v.Machine, v.IP, v.OnExternalSource, v.ExternalName, v.FileType, v.FileMIME)

		if err != nil {
			return err
		}
	}
	tx.Commit()
	bar.FinishPrint("done ...")

	err = deleteTemporaryFile(filename)
	if err != nil {
		return err
	}

	return nil

}

func deleteTemporaryFile(filename string) (err error) {
	err = os.Remove(filename)
	return
}

func ListFiles(w http.ResponseWriter, req *http.Request) {
	var files Files

	rows, err := db.Query("select id, name, path, size, isdir, machine, ip, onexternalsource, externalname, filetype, filemime from files")
	if err != nil {
		log.Println("ERROR", err)
	}
	defer rows.Close()
	for rows.Next() {
		var file File
		err = rows.Scan(&file.ID, &file.Name, &file.Path, &file.Size, &file.IsDir, &file.Machine, &file.IP, &file.OnExternalSource, &file.ExternalName, &file.FileType, &file.FileMIME)
		if err != nil {
			log.Println(err)
		}
		files = append(files, file)
	}
	err = rows.Err()
	if err != nil {
		log.Println(err)
	}

	json.NewEncoder(w).Encode(&files)
}

func ByteCountSI(b int64) string {
	const unit = 1000
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB",
		float64(b)/float64(div), "kMGTPE"[exp])
}
