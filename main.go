package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"

	_ "github.com/mattn/go-sqlite3"
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
	db, err = sql.Open("sqlite3", "./gofi.db")
	if err != nil {
		log.Println(err)
	}

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
}

/* A Simple function to verify error */
func CheckError(err error) {
	if err != nil {
		fmt.Println("Error: ", err)
		os.Exit(0)
	}
}

func main() {
	ServerAddr, err := net.ResolveUDPAddr("udp", ":1985")
	CheckError(err)

	ServerConn, err := net.ListenUDP("udp", ServerAddr)
	CheckError(err)
	defer ServerConn.Close()

	buf := make([]byte, 1024)

	var file File

	for {
		n, _, err := ServerConn.ReadFromUDP(buf)

		r := bytes.NewReader(buf[0:n])

		decoder := json.NewDecoder(r)
		err = decoder.Decode(&file)

		if err != nil {
			log.Println(err)
		}

		fileCount++
		addFile(file)

		if err != nil {
			fmt.Println("Error: ", err)
		}
		fmt.Printf("\rRecieved %d files", fileCount)
	}
}

func addFile(file File) (bool, error) {
	stmt, err = db.Prepare("INSERT OR IGNORE INTO files(name, path, size, isdir, machine, ip) values(?,?,?,?,?,?)")

	if err != nil {
		return false, err
	}

	_, err = stmt.Exec(file.Name, file.Path, file.Size, file.IsDir, file.Machine, file.IP)

	if err != nil {
		return false, err
	}

	return true, nil
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
