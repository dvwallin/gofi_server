package main

import (
	"database/sql"
	"fmt"
	"html/template"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/gorilla/mux"
	_ "github.com/mattn/go-sqlite3"
	pb "gopkg.in/cheggaaa/pb.v1"
)

const (
	BUFFERSIZE         = 2048
	SERVER_PORT        = 1985
	GOFI_DATABASE_NAME = "gofi.db"
)

var (
	db        *sql.DB
	err       error
	stmt      *sql.Stmt
	res       sql.Result
	fileCount int = 0
	tmpl      *template.Template
)

type (
	File struct {
		ID               string `json:"id,omitempty"`
		Name             string `json:"name,omitempty"`
		Path             string `json:"path,omitempty"`
		Size             int64  `json:"size"`
		HumanSize        string
		IsDir            int    `json:"isdir"`
		Machine          string `json:"machine"`
		IP               string `json:"ip"`
		OnExternalSource int    `json:"on_external_source"`
		ExternalName     string `json:"external_name"`
		FileType         string `json:"file_type"`
		FileMIME         string `json:"file_mime"`
		FileHash         string `json:"filehash"`
		Modified         string `json:"modified"`
	}
	Files []File

	PageData struct {
		PageTitle    string
		Files        Files
		TotalResults int
		Limit        string
		Asc          bool
		Filetypes    []string
		Machines     []string
		FileMimes    []string
		Vars         Vars
		FilterParts  []string
	}

	Vars struct {
		Limit    string
		OrderBy  string
		Order    string
		Filetype string
		Machine  string
		Filemime string
	}
)

func init() {

	// Connect to the database
	db, err = sql.Open("sqlite3", fmt.Sprintf("./%s", GOFI_DATABASE_NAME))
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
				onexternalsource integer NOT NULL,
				externalname text NOT NULL,
				filetype text NOT NULL,
				filemime text NOT NULL,
        filehash text NOT NULL,
        modified text NOT NULL,
			CONSTRAINT path_unique UNIQUE (path, machine, ip, onexternalsource, externalname, filehash)
			);
	`
	_, err = db.Exec(sqlStmt)
	if err != nil {
		log.Printf("%q: %s\n", err, sqlStmt)
		return
	}

	tmpl = template.Must(template.ParseFiles("templates/index.html"))
}

func main() {
	go func() {
		r := mux.NewRouter()
		r.HandleFunc("/", ListFiles)

		http.ListenAndServe(":1980", r)
	}()
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

		// if err == nil {
		// 	log.Println("removing temporary file ...")
		// 	err = deleteTemporaryFile(filepath.Join(GOFI_TMP_DIR, filename))
		// 	if err != nil {
		// 		log.Println("error deleting temporary file", err)
		// 	}
		// }
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

	rows, err := tempDB.Query("SELECT name, path, size, isdir, machine, ip, onexternalsource, externalname, filetype, filemime, filehash, modified FROM files")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var file File
		err = rows.Scan(&file.Name, &file.Path, &file.Size, &file.IsDir, &file.Machine, &file.IP, &file.OnExternalSource, &file.ExternalName, &file.FileType, &file.FileMIME, &file.FileHash, &file.Modified)
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

		stmt, err = tx.Prepare("INSERT OR IGNORE INTO files(name, path, size, isdir, machine, ip, onexternalsource, externalname, filetype, filemime, filehash, modified) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)")

		if err != nil {
			return err
		}

		_, err = stmt.Exec(v.Name, v.Path, v.Size, v.IsDir, v.Machine, v.IP, v.OnExternalSource, v.ExternalName, v.FileType, v.FileMIME, v.FileHash, v.Modified)

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
	v := req.URL.Query()
	vars := Vars{}

	var extraSQLSlice []string
	var extraSQL string
	var filterParts []string

	// get Limit
	reg, err := regexp.Compile("[^0-9]+")
	if err != nil {
		log.Fatal(err)
	}
	vars.Limit = reg.ReplaceAllString(v.Get("limit"), "")

	if vars.Limit == "" || len(vars.Limit) > 4 {
		vars.Limit = "100"
	}

	filterParts = append(filterParts, fmt.Sprintf("limit: %s", vars.Limit))

	// -- limit end

	// get order_by
	reg, err = regexp.Compile("[^a-zA-Z]+")
	if err != nil {
		log.Fatal(err)
	}
	vars.OrderBy = reg.ReplaceAllString(v.Get("order_by"), "")

	if vars.OrderBy == "" {
		vars.OrderBy = "name"
	}
	filterParts = append(filterParts, fmt.Sprintf("ordering by: %s", vars.OrderBy))

	// -- order_by end

	// get order
	reg, err = regexp.Compile("[^a-zA-Z]+")
	if err != nil {
		log.Fatal(err)
	}
	vars.Order = reg.ReplaceAllString(v.Get("order"), "")
	if vars.Order == "asc" {
		vars.Order = "asc"
	} else {
		vars.Order = "desc"
	}
	filterParts = append(filterParts, fmt.Sprintf("order: %s", vars.Order))

	// -- order end

	// get filetype
	reg, err = regexp.Compile("[^a-zA-Z]+")
	if err != nil {
		log.Fatal(err)
	}
	vars.Filetype = reg.ReplaceAllString(v.Get("filetype"), "")

	if vars.Filetype != "" && vars.Filetype != "*" {
		extraSQLSlice = append(extraSQLSlice, fmt.Sprintf("filetype=\"%s\"", vars.Filetype))
		filterParts = append(filterParts, fmt.Sprintf("filetype: %s", vars.Filetype))
	}

	// -- filetype end

	// -- machines end

	// get machines
	reg, err = regexp.Compile("[^a-zA-Z]+")
	if err != nil {
		log.Fatal(err)
	}
	vars.Machine = reg.ReplaceAllString(v.Get("machine"), "")

	if vars.Machine != "" && vars.Machine != "*" {
		extraSQLSlice = append(extraSQLSlice, fmt.Sprintf("machine=\"%s\"", vars.Machine))
		filterParts = append(filterParts, fmt.Sprintf("machine: %s", vars.Machine))
	}

	// -- machines end

	// -- filemimes end

	// get Filemime
	reg, err = regexp.Compile("[^a-zA-Z0-9/;\\-=_ ]+")
	if err != nil {
		log.Fatal(err)
	}
	fmime, err := url.QueryUnescape(v.Get("filemime"))
	if err != nil {
		log.Fatal(err)
	}
	vars.Filemime = reg.ReplaceAllString(fmime, "")

	if vars.Filemime != "" && vars.Filemime != "*" {
		extraSQLSlice = append(extraSQLSlice, fmt.Sprintf("filemime=\"%s\"", vars.Filemime))
		filterParts = append(filterParts, fmt.Sprintf("filemime: %s", vars.Filemime))
	}

	// -- filemimes end

	// building sql -addition

	if len(extraSQLSlice) > 0 {
		extraSQL = fmt.Sprintf(" WHERE %s", strings.Join(extraSQLSlice, " AND "))
	}

	sql := fmt.Sprintf("SELECT id, name, path, size, isdir, machine, ip, onexternalsource, externalname, filetype, filemime, filehash, modified FROM files%s ORDER BY %s %s LIMIT %s", extraSQL, vars.OrderBy, vars.Order, vars.Limit)

	var files Files

	rows, err := db.Query(sql)
	if err != nil {
		log.Println("ERROR", err)
	}
	defer rows.Close()
	for rows.Next() {
		var file File
		err = rows.Scan(&file.ID, &file.Name, &file.Path, &file.Size, &file.IsDir, &file.Machine, &file.IP, &file.OnExternalSource, &file.ExternalName, &file.FileType, &file.FileMIME, &file.FileHash, &file.Modified)
		if err != nil {
			log.Println(err)
		}
		file.HumanSize = ByteCountDecimal(file.Size)
		files = append(files, file)
	}

	err = rows.Err()
	if err != nil {
		log.Println(err)
	}

	var asc bool
	if vars.Order == "asc" {
		asc = true
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	data := PageData{
		PageTitle:    "GOFI Server",
		Files:        files,
		TotalResults: len(files),
		Limit:        vars.Limit,
		Asc:          asc,
		Filetypes:    filetypes(),
		Machines:     machines(),
		FileMimes:    filemimes(),
		Vars:         vars,
		FilterParts:  filterParts,
	}
	tmpl.Execute(w, data)
}

func filemimes() (filemimes []string) {
	sql := "SELECT DISTINCT filemime FROM files order by filemime asc"

	rows, err := db.Query(sql)
	if err != nil {
		log.Println("ERROR", err)
	}
	defer rows.Close()
	for rows.Next() {
		var filemime string
		err = rows.Scan(&filemime)
		if err != nil {
			log.Println(err)
		}
		filemimes = append(filemimes, filemime)
	}

	err = rows.Err()
	if err != nil {
		log.Println(err)
	}
	return filemimes
}

func filetypes() (filetypes []string) {
	sql := "SELECT DISTINCT filetype FROM files order by filetype asc"

	rows, err := db.Query(sql)
	if err != nil {
		log.Println("ERROR", err)
	}
	defer rows.Close()
	for rows.Next() {
		var filetype string
		err = rows.Scan(&filetype)
		if err != nil {
			log.Println(err)
		}
		filetypes = append(filetypes, filetype)
	}

	err = rows.Err()
	if err != nil {
		log.Println(err)
	}
	return filetypes
}

func machines() (machines []string) {
	sql := "SELECT DISTINCT machine FROM files order by machine asc"

	rows, err := db.Query(sql)
	if err != nil {
		log.Println("ERROR", err)
	}
	defer rows.Close()
	for rows.Next() {
		var machine string
		err = rows.Scan(&machine)
		if err != nil {
			log.Println(err)
		}
		machines = append(machines, machine)
	}

	err = rows.Err()
	if err != nil {
		log.Println(err)
	}
	return machines
}

func IsNumeric(s string) bool {
	_, err := strconv.ParseFloat(s, 64)
	return err == nil
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

func ByteCountDecimal(b int64) string {
	const unit = 1000
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "kMGTPE"[exp])
}
