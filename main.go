package main

import (
	"database/sql"
	"fmt"
	"io/ioutil"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
	"embed"
	"github.com/gin-contrib/static"
	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"
)

// user configurable variables
const (
	dbName           = "database.db" // filename for sqlite3 DB
	linkSize         = 6             //
	uploadLimit      = 25            // in MiB
	filesRoot        = "uploads"     // storage for files / pastes
	listenAddr       = 8086          // files / interface port
	applicationTitle = "FoSSBin"
	theme            = "github-gist"
	username         = "magnum"
	password         = "password"
)

// db entry types
const (
	FILE = iota
	PASTE
	URL
)

type uploadObject struct {
	id         int
	url        string
	uploadType int
	password   string
	param      string
}

type linkCreateParams struct {
	LongURL string `json:"long_url" binding:"required"`
}

type pasteCreateParams struct {
	PasteData string `json:"paste_data" binding:"required"`
	Password  string `json:"password"`
}

func createEntry(database *sql.DB, object *uploadObject) {
	statement, _ := database.Prepare(
		"INSERT INTO " +
			"uploads (url, uploadType, password, param) " +
			"VALUES (?, ?, ?, ?)")
	_, _ = statement.Exec(object.url, object.uploadType, object.password, object.param)

}

func getEntry(database *sql.DB, url string) (*uploadObject, error) {
	rows := database.QueryRow(""+
		"SELECT id, url, uploadType, password, param "+
		"FROM uploads WHERE url=?;", url)

	var obj = new(uploadObject)

	err := rows.Scan(&obj.id, &obj.url, &obj.uploadType, &obj.password, &obj.param)
	if err != nil {
		return nil, err
	}
	return obj, nil
}

func connectToDb() *sql.DB {
	database, err := sql.Open("sqlite3", dbName)
	if err != nil {
		panic(err)
	}
	statement, _ := database.Prepare("" +
		"CREATE TABLE IF NOT EXISTS " +
		"uploads (id INTEGER PRIMARY KEY , url VARCHAR, " +
		"uploadType INTEGER, password VARCHAR, " +
		"param VARCHAR);")

	_, err = statement.Exec()
	if err != nil {
		panic(err)
	}
	return database
}

func checkIfAlreadyExists(db *sql.DB, url string) bool {
	_, err := getEntry(db, url)
	return err != sql.ErrNoRows
}

func getUniqueLink(db *sql.DB) string {
	link := generateLink()

	for checkIfAlreadyExists(db, link) {
		link = generateLink()
	}
	return link
}

func generateLink() string {
	alphabet := "abcdefghijklmnopqrstuvwxz" +
		"1234567890"

	var sb strings.Builder
	k := len(alphabet)
	for i := 0; i < linkSize; i++ {
		c := alphabet[rand.Intn(k)]
		sb.WriteByte(c)
	}
	return sb.String()

}

func checkOrCreateDirectory() {
	if _, err := os.Stat(filesRoot); os.IsNotExist(err) {
		err := os.Mkdir(filesRoot, 0755)
		if err != nil {
			panic(fmt.Sprintf("Could not create files directory. Error : %v", err))
		}
	}
}

func main() {
	// seed the random number generator to generate short links
	rand.Seed(time.Now().Unix())
	gin.SetMode(gin.ReleaseMode)
	// initialize connection to db
	db := connectToDb()

	// ensure that the directory to upload files exists
	checkOrCreateDirectory()

	// initialize the gin router
	router := gin.Default()

	// load templates
	router.LoadHTMLGlob("templates/*")

	// set the multipart limit
	router.MaxMultipartMemory = uploadLimit << 20 // 25 MiB is default

	// handle authentication
	// authorized := router.Group("/", gin.BasicAuth(gin.Accounts{
	// 	username: password,
	// }))
	authorized := router

	// handle uploading files to the server
	authorized.POST("/upload", func(c *gin.Context) {
		// Multipart form
		form, _ := c.MultipartForm()

		// get the uploaded file
		files := form.File["file"]
		file := files[0]

		// ensure filesize is within limits
		if file.Size > uploadLimit<<20 {
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "The file size is too large, upload failed."})
			return
		}

		// create a new entry in db
		var newEntry = new(uploadObject)
		newEntry.url = getUniqueLink(db)
		newEntry.param = file.Filename
		newEntry.uploadType = FILE
		createEntry(db, newEntry)

		err := c.SaveUploadedFile(file, fmt.Sprintf("%v/", filesRoot)+newEntry.url)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "The file could not be saved."})
		}

		c.JSON(http.StatusOK, gin.H{
			"status": "success",
			"url":    newEntry.url,
		})
	})

	// handle creation of short links
	authorized.POST("/link", func(c *gin.Context) {

		// get long url from post parameters
		var par linkCreateParams
		err := c.BindJSON(&par)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": err.Error(),
			})
			return
		}

		// add new entry to db
		var obj = new(uploadObject)
		obj.url = getUniqueLink(db)
		obj.param = par.LongURL
		obj.uploadType = URL
		createEntry(db, obj)

		c.JSON(http.StatusOK, gin.H{
			"status": "success",
			"url":    obj.url,
		})
	})

	// handle uploading of paste data
	authorized.POST("/paste", func(c *gin.Context) {

		// get long url from post parameters
		var par pasteCreateParams
		err := c.BindJSON(&par)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": err.Error(),
			})
			return
		}

		// add new entry to db
		var obj = new(uploadObject)
		obj.url = getUniqueLink(db)
		obj.uploadType = PASTE
		createEntry(db, obj)

		// create file with paste contents

		// check already existing file
		_, err = os.Stat(filepath.FromSlash(
			filesRoot + "/" + obj.url,
		))
		if err == nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"status": "File already exists. Please run cleaner.",
			})
			return
		}

		// create a file with paste data
		err = ioutil.WriteFile(filepath.FromSlash(filesRoot+"/"+obj.url), []byte(par.PasteData), 0644)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"status": "Unable to create file",
				"error":  err.Error(),
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"status": "success",
			"url":    obj.url,
		})
	})

	// handle fetches
	router.GET("/:shortUrl", func(c *gin.Context) {
		shortURL := c.Param("shortUrl")

		obj, err := getEntry(db, shortURL)
		if err != nil {
			if err == sql.ErrNoRows {
				c.String(http.StatusNotFound, "404 page not found")
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": "unable to fetch resource",
			})
			return
		}
		switch obj.uploadType {
		case URL:
			c.Redirect(http.StatusFound, obj.param)
			return
		case FILE:
			//dat, err := ioutil.ReadFile(filepath.FromSlash(filesRoot + "/" + shortUrl))
			//if err != nil {
			//	c.JSON(http.StatusInternalServerError, gin.H{
			//		"error": "unable to fetch resource",
			//	})
			//	return
			//}
			//
			//c.Header("Content-Type", http.DetectContentType(dat[:600]))
			//c.Header("Content-Disposition", "inline")
			//c.Header("Content-Length", string(rune(len(dat))))
			c.File(filesRoot + "/" + shortURL)
			return
		case PASTE:
			if queryParam, ok := c.GetQuery("raw"); ok {
				if queryParam == "1" {
					c.File(filesRoot + "/" + shortURL)
					return
				}
			}

			dat, err := ioutil.ReadFile(filepath.FromSlash(filesRoot + "/" + shortURL))
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"error": "unable to fetch resource",
				})
				return
			}

			c.HTML(http.StatusOK, "view_paste.html", gin.H{
				"title": applicationTitle,
				"code":  string(dat),
				"theme": theme,
			})
			return
		}

	})

	// handle static files
	router.Use(static.Serve("/static", static.LocalFile("./static", false)))

	// application root handler
	router.GET("/", func(c *gin.Context) {
		c.File(filepath.FromSlash("static/index.html"))
		return
	})

	// start the server
	err := router.Run(fmt.Sprintf(":%v", listenAddr))
    fmt.Println("%s", "Server is started and listening.")
	if err != nil {
		panic("Unable to start server")
	}
}
