package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	_ "github.com/mattn/go-sqlite3"
	"gopkg.in/cheggaaa/pb.v1"
)

const (
	BUFFERSIZE         = 2048
	SERVER_PORT        = 1985
	GOFI_DATABASE_NAME = "gofi.db"
	GOFI_TMP_DIR       = ".gofi_tmp/"
)

var (
	db        *sql.DB
	err       error
	stmt      *sql.Stmt
	res       sql.Result
	fileCount int = 0
)

type (
	File struct {
		ID      string `json:"id,omitempty"`
		Name    string `json:"name,omitempty"`
		Path    string `json:"path,omitempty"`
		Size    int64  `json:"size"`
		IsDir   int    `json:"isdir"`
		Machine string `json:"machine"`
		IP      string `json:"ip"`
	}
	Files []File
)

func init() {

	// Connect to the database
	db, err = sql.Open("sqlite3", fmt.Sprintf("./%s", GOFI_DATABASE_NAME))
	if err != nil {
		log.Println(err)
	}

	// Make sure the correct scheme exists
	sqlStmt := `
		CREATE TABLE IF NOT EXISTS files 
			(	id integer NOT NULL primary key, 
				name text NOT NULL, 
				path text NOT NULL, 
				size integer NOT NULL, 
				isdir integer NOT NULL, 
				machine text NOT NULL, 
				ip text NOT NULL, 
			CONSTRAINT path_unique UNIQUE (path, machine, ip)
			);
	`
	_, err = db.Exec(sqlStmt)
	if err != nil {
		log.Printf("%q: %s\n", err, sqlStmt)
		return
	}

	// Create the GOFI_TMP_DIR in case it does not exist already
	newpath := filepath.Join(".", GOFI_TMP_DIR)
	os.MkdirAll(newpath, os.ModePerm)
}

func main() {
	server, err := net.Listen("tcp", fmt.Sprintf(":%d", SERVER_PORT))
	if err != nil {
		log.Println("error listetning: ", err)
		os.Exit(1)
	}
	defer server.Close()
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

		if err == nil {
			log.Println("removing temporary file ...")
			err = deleteTemporaryFile(filepath.Join(GOFI_TMP_DIR, filename))
			if err != nil {
				log.Println("error deleting temporary file", err)
			}
		}
		log.Println("transaction ended ...")
	}
}

func getFile(connection net.Conn) (filename string, err error) {
	bufferFileName := make([]byte, 64)
	bufferFileSize := make([]byte, 10)

	connection.Read(bufferFileSize)
	fileSize, _ := strconv.ParseInt(strings.Trim(string(bufferFileSize), ":"), 10, 64)

	connection.Read(bufferFileName)
	filename = strings.Trim(string(bufferFileName), ":")

	newFile, err := os.Create(filepath.Join(GOFI_TMP_DIR, filename))
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
	return filename, nil
}

func addFile(filename string) (err error) {

	log.Println("initiating saving files to database ...")

	var files Files

	file, err := os.Open(filepath.Join(GOFI_TMP_DIR, filename))
	if err != nil {
		return err
	}

	data, err := ioutil.ReadAll(file)
	if err != nil {
		return err
	}

	err = json.Unmarshal(data, &files)
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

		stmt, err = tx.Prepare("INSERT OR IGNORE INTO files(name, path, size, isdir, machine, ip) values(?,?,?,?,?,?)")

		if err != nil {
			return err
		}

		_, err = stmt.Exec(v.Name, v.Path, v.Size, v.IsDir, v.Machine, v.IP)

		if err != nil {
			return err
		}
	}
	tx.Commit()
	bar.FinishPrint("done ...")

	return nil

}

func deleteTemporaryFile(filename string) (err error) {
	err = os.Remove(filename)
	return
}

// func ListFiles(w http.ResponseWriter, req *http.Request) {
// 	var files Files

// 	rows, err := db.Query("select id, name, path, size, isdir, machine, ip from files")
// 	if err != nil {
// 		log.Println("ERROR", err)
// 	}
// 	defer rows.Close()
// 	for rows.Next() {
// 		var file File
// 		err = rows.Scan(&file.ID, &file.Name, &file.Path, &file.Size, &file.IsDir, &file.Machine, &file.IP)
// 		if err != nil {
// 			log.Println(err)
// 		}
// 		files = append(files, file)
// 	}
// 	err = rows.Err()
// 	if err != nil {
// 		log.Println(err)
// 	}

// 	json.NewEncoder(w).Encode(&files)
// }
