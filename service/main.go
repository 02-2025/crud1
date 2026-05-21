package main

import (
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/user"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"
)

// ROOT_DIR переменная окружения корневой директории в контейнере docker
var rootDir = os.Getenv("ROOT_DIR")

// структура для отправки ответа на запрос через функцию sendResponse()
type Response struct {
	Success bool   `json:"success"`
	Content any    `json:"content,omitempty"`
	Error   string `json:"error,omitempty"`
}

// функция отправки json ответа на запрос
func sendResponse(w http.ResponseWriter, data any, statusCode int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(data)
}

func logRequest(r *http.Request, start time.Time) {
	duration := time.Since(start).Milliseconds()
	log.Printf("[%s] %s %dms", r.Method, r.URL.Path, duration)
}

func logError(r *http.Request, err error) {
	log.Printf("[ERROR] %s %s | %v", r.Method, r.URL.Path, err)
}

// функция приводит строковый путь path в безопасную форму, и возвращает абсолютный путь,
// полученный путём присоединения корневой директории root;
// полученный путь исключает возможность выхода за пределы корневой директории;
// функция требует, чтобы в качестве аргумента path был передан абсолютный путь (начинающийся с /),
// иначе будет возвращена ошибка
func pathGuard(root, path string) (string, error) {
	cleanPath := filepath.Clean(path)
	if !filepath.IsAbs(cleanPath) {
		return "", errors.New("Path must be absolute or path is empty")
	}
	absPath := filepath.Join(root, cleanPath)
	if _, err := filepath.Rel(root, absPath); err != nil {
		return "", err
	}
	return absPath, nil
}

// проверка вложенности пути src в dst
// используется для предотвращения копирования/перемещения директории в себя же
func sublevelCheck(src, dst string) bool {
	rel, err := filepath.Rel(src, dst)
	if err != nil || rel == "." {
		return false
	}
	return !strings.HasPrefix(rel, "..")
}

// файл по пути path существует: true;
// файл не существует: false
func existenceCheck(path string) bool {
	_, err := os.Stat(path)
	return !os.IsNotExist(err)
}

// файл по пути path — директория: true;
// файл не директория: false
func isDir(path string) (bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		return false, err
	}

	return info.IsDir(), nil
}

// структура для отправки метаданных файла в ответе на запрос
type FileInfo struct {
	Name    string      `json:"name"`
	IsDir   bool        `json:"isdir"`
	Type    string      `json:"type"`
	Size    int64       `json:"size"`
	ModTime string      `json:"modtime"`
	Mode    os.FileMode `json:"mode"`
	Owner   string      `json:"owner,omitempty"`
}

// структура для отправки статистики текстового файла в ответе на запрос
type FileStats struct {
	Words       int `json:"words"`
	UniqueWords int `json:"uniquewords"`
	Chars       int `json:"chars"`
	Lines       int `json:"lines"`
}

// интерфейс, который объединяет сигнатуры fs.FileInfo и os.DirEntry интерфейсов;
// используется для правильной обработки выше упомянутых интерфейсов в функции getFileInfo
type file interface {
	Info() (fs.FileInfo, error)
	Name() string
	IsDir() bool
}

// структура, которая будет реализовывать интерфейс file
type fileAd struct {
	fs.FileInfo
}

// реализация метода интерфейса file, для его работы;
// возвращает интерфейс fs.FileInfo, описывающий файл
func (f fileAd) Info() (fs.FileInfo, error) {
	return f.FileInfo, nil
}

// вспомогательная функция сравнения строк a и b, для функции сортировки
func compareStrings(a, b string, order string) bool {
	if order == "desc" {
		return strings.ToLower(a) > strings.ToLower(b)
	}
	return strings.ToLower(a) < strings.ToLower(b)
}

// функция для заполнения структуры FileInfo для отправки в ответе на запрос
func getFileInfo(file file) (FileInfo, error) {
	info, err := file.Info()
	if err != nil {
		return FileInfo{}, err
	}
	stat := info.Sys().(*syscall.Stat_t)

	u, err := user.LookupId(strconv.FormatInt(int64(stat.Uid), 10))
	owner := "unknown"
	if err == nil {
		owner = u.Username
	}
	return FileInfo{
		Name:    file.Name(),
		IsDir:   file.IsDir(),
		Type:    filepath.Ext(file.Name()),
		Size:    info.Size(),
		ModTime: info.ModTime().Format(time.RFC3339),
		Mode:    info.Mode(),
		Owner:   owner,
	}, nil
}

// функция подсчёта статистики текстового файла:
// слова, уникальные слова, символы, строки
func getFileStats(path string) (FileStats, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return FileStats{}, err
	}

	text := string(data)
	lines := strings.Count(text, "\n")
	words := len(strings.Fields(text))

	unique := make(map[string]struct{})
	for _, word := range strings.Fields(strings.ToLower(text)) {
		unique[word] = struct{}{}
	}

	return FileStats{
		Words:       words,
		UniqueWords: len(unique),
		Chars:       utf8.RuneCountInString(text),
		Lines:       lines,
	}, nil
}

// функция рекурсивного поиска по имени, внутри указанной директории
func searchFilesByName(root string, query string) ([]FileInfo, error) {
	// обработку ошибок в лог
	var results []FileInfo
	filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if strings.Contains(strings.ToLower(d.Name()), strings.ToLower(query)) {
			info, _ := d.Info()
			temp, err := getFileInfo(fileAd{info})
			temp.Name = path
			if err != nil {
				return err
			}
			results = append(results, temp)
		}
		return nil
	})
	return results, nil
}

// копирование файла с src путём, по пути dst, включая вложенные файлы, если источник директория
func copyRecursive(src, dst string) error {
	srcInfo, err := os.Stat(src)
	if err != nil {
		return err
	}

	append := ""
	if src == dst {
		append = " — copy"
	}

	if srcInfo.IsDir() {
		if err := os.MkdirAll(dst, srcInfo.Mode()); err != nil {
			return err
		}
		srcDirInfo, err := os.ReadDir(src)
		if err != nil {
			return err
		}
		for _, file := range srcDirInfo {
			srcPath := filepath.Join(src, file.Name())
			dstPath := filepath.Join(dst, file.Name())
			if file.IsDir() {
				if err := copyRecursive(srcPath, dstPath); err != nil {
					return err
				}
			} else {
				if err := copyFile(srcPath, dstPath, append); err != nil {
					return err
				}
			}
		}
	} else {
		if err := copyFile(src, dst, append); err != nil {
			return err
		}
	}
	return nil
}

// копирование файла путём открытия для чтения источника src, создания файла назначения dst
// и копирования содержимого из файла с src путём в файл с dst путём;
func copyFile(src, dst, append string) error {
	tempAppend := append
	from, err := os.Open(src)
	if err != nil {
		return err
	}
	defer from.Close()

	srcInfo, err := from.Stat()
	if err != nil {
		return err
	}

	for existenceCheck(dst+append) && append != "" {
		append += tempAppend
	}
	// to, err := os.OpenFile(dst+append, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, srcInfo.Mode())
	to, err := os.Create(dst + append)
	if err != nil {
		return err
	}
	defer to.Close()

	if _, err = io.Copy(to, from); err != nil {
		return err
	}
	if err := os.Chmod(dst+append, srcInfo.Mode()); err != nil {
		return err
	}

	return nil
}

// функция сравнивает идентификаторы устройств файловой системы файла источника srcInfo и файла назначения dstInfo
func devsSamenessCheck(srcInfo, dstInfo os.FileInfo) bool {
	srcStat := srcInfo.Sys().(*syscall.Stat_t)
	dstStat := dstInfo.Sys().(*syscall.Stat_t)

	if srcStat.Dev != dstStat.Dev {
		return false
	}

	return true
}

// меняет путь файлу с абсолютным путём src из первого аргумента на путь dst из второго (переименовывает);
// если os.Rename() возвращает ошибку:
// в зависимости от результата проверки "одинаковости" идентификаторов устройств ФС файлов:
// различается: вызывает функцию копирования, а после удаления оригинала
// совпадает: выходит с ошибкой
func move(src, dst string) error {

	if err := os.Rename(src, dst); err == nil {
		return nil
	}

	// получение os.Fileinfo файлов для дальнейшего сравнения информации
	srcInfo, err := os.Stat(src)
	if err != nil {
		return err
	}
	dstInfo, err := os.Stat(filepath.Dir(dst))
	if err != nil {
		return err
	}

	if devsSamenessCheck(srcInfo, dstInfo) {
		return errors.New("Internal server error")
	}

	if err := copyRecursiveForMove(src, dst); err != nil {
		return err
	}

	if err := os.RemoveAll(src); err != nil {
		return err
	}

	return nil
}

// копирование файла с src путём, по пути dst, включая вложенные файлы, если источник директория
// отличается от copyRecursive() отсутствием логики для добавления строки в конец имени файла
// используется только когда индентификаторы устройств файловых систем источника и назначения различны
func copyRecursiveForMove(src, dst string) error {
	srcInfo, err := os.Stat(src)
	if err != nil {
		return err
	}

	if srcInfo.IsDir() {
		if err := os.MkdirAll(dst, srcInfo.Mode()); err != nil {
			return err
		}
		srcDirInfo, err := os.ReadDir(src)
		if err != nil {
			return err
		}
		for _, file := range srcDirInfo {
			srcPath := filepath.Join(src, file.Name())
			dstPath := filepath.Join(dst, file.Name())
			if file.IsDir() {
				if err := copyRecursiveForMove(srcPath, dstPath); err != nil {
					return err
				}
			} else {
				if err := copyFileForMove(srcPath, dstPath); err != nil {
					return err
				}
			}
		}
	} else {
		if err := copyFileForMove(src, dst); err != nil {
			return err
		}
	}

	return nil
}

// копирование файла путём открытия для чтения источника src, создания файла назначения dst
// и копирования содержимого из src в dst;
// является вспомогательной для copyRecursiveForMove()
// отличается от copyFile() отсутствием логики для добавления строки в конец имени файла
func copyFileForMove(src, dst string) error {
	from, err := os.Open(src)
	if err != nil {
		return err
	}
	defer from.Close()

	srcInfo, err := from.Stat()
	if err != nil {
		return err
	}

	// to, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, srcInfo.Mode())
	to, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer to.Close()

	if _, err = io.Copy(to, from); err != nil {
		return err
	}
	if err := os.Chmod(dst, srcInfo.Mode()); err != nil {
		return err
	}

	return nil
}

// функция записывает данные content во временный файл в родительской директории файла с путём absPath,
// после заменяет файл с путём absPath на временный — переименовывает временный файл;
// в случае ошибки временный файл с данными content остаётся
func write(absPath string, content string, append bool) error {
	origInfo, err := os.Stat(absPath)
	if err != nil {
		return err
	}

	if append {
		f, err := os.OpenFile(absPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, origInfo.Mode().Perm())
		if err != nil {
			return err
		}
		defer f.Close()

		if _, err := f.WriteString(content); err != nil {
			return err
		}

	} else {
		tempFile, err := os.CreateTemp(filepath.Dir(absPath), "."+filepath.Base(absPath)+".temp-*")
		if err != nil {
			return err
		}

		// defer os.Remove(tempFile.Name())
		// defer tempFile.Close()

		if _, err := tempFile.WriteString(content); err != nil {
			return err
		}

		if err := os.Chmod(tempFile.Name(), origInfo.Mode()); err != nil {
			return err
		}

		if origInfo.IsDir() {
			return errors.New("Cannot write to a directory. Not written data stored in the temp file: " + tempFile.Name())
		}

		if err := os.Rename(tempFile.Name(), absPath); err != nil {
			return errors.New(err.Error() + ". Not written data stored in the temp file: " + tempFile.Name())
		}

	}
	return nil
}

// ОБРАБОТЧИКИ HTTP ЗАПРОСОВ
func readFileHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		sendResponse(w, Response{Success: false, Error: "Method not allowed"}, http.StatusMethodNotAllowed)
		return
	}
	start := time.Now()
	defer logRequest(r, start)

	relPath := r.URL.Query().Get("path")
	absPath, err := pathGuard(rootDir, relPath)

	if err != nil { // || absPath == "" {
		sendResponse(w, Response{Success: false, Error: err.Error()}, http.StatusBadRequest)
		return
	}
	if !existenceCheck(absPath) {
		sendResponse(w, Response{Success: false, Error: "File not found"}, http.StatusBadRequest)
		return
	}

	content, err := os.ReadFile(absPath)
	if err != nil {
		logError(r, err)
		sendResponse(w, Response{Success: false, Error: err.Error()}, http.StatusInternalServerError)
		return
	}
	sendResponse(w, Response{Success: true, Content: string(content)}, http.StatusOK)

}

func getInfoHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		sendResponse(w, Response{Success: false, Error: "Method not allowed"}, http.StatusMethodNotAllowed)
		return
	}
	start := time.Now()
	defer logRequest(r, start)

	path := r.URL.Query().Get("path")
	absPath, err := pathGuard(rootDir, path)

	if err != nil { // || absPath == "" {
		sendResponse(w, Response{Success: false, Error: err.Error()}, http.StatusBadRequest)
		return
	}

	if !existenceCheck(absPath) {
		sendResponse(w, Response{Success: false, Error: "File not found"}, http.StatusBadRequest)
		return
	}

	file, err := os.Stat(absPath)
	if err != nil {
		sendResponse(w, Response{Success: false, Error: err.Error()}, http.StatusInternalServerError)
		return
	}
	fileInfo, err := getFileInfo(fileAd{file})
	if err != nil {
		logError(r, err)
		sendResponse(w, Response{Success: false, Error: err.Error()}, http.StatusInternalServerError)
		return
	}

	sendResponse(w, Response{Success: true, Content: fileInfo}, http.StatusOK)
}

func getListHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		sendResponse(w, Response{Success: false, Error: "Method not allowed"}, http.StatusMethodNotAllowed)
		return
	}
		start := time.Now()
	defer logRequest(r, start)

	path := r.URL.Query().Get("path")
	absPath, err := pathGuard(rootDir, path)
	if err != nil { // || absPath == "" {
		sendResponse(w, Response{Success: false, Error: err.Error()}, http.StatusBadRequest)
		return
	}

	if !existenceCheck(absPath) {
		sendResponse(w, Response{Success: false, Error: "Directory not found"}, http.StatusBadRequest)
		return
	}

	if isDir, err := isDir(absPath); err != nil {
		sendResponse(w, Response{Success: false, Error: err.Error()}, http.StatusInternalServerError)
		return
	} else if !isDir {
		sendResponse(w, Response{Success: false, Error: "Not a directory"}, http.StatusBadRequest)
		return
	}

	dirList, err := os.ReadDir(absPath)
	if err != nil {
		sendResponse(w, Response{Success: false, Error: err.Error()}, http.StatusInternalServerError)
		return
	}
	var files []FileInfo
	for _, entry := range dirList {
		file, err := getFileInfo(entry)
		if err != nil {
			logError(r, err)
			sendResponse(w, Response{Success: false, Error: err.Error()}, http.StatusInternalServerError)
			return
		}
		files = append(files, file)
	}

	// name, size, date, type
	sortBy := r.URL.Query().Get("sort")
	// asc, desc
	order := r.URL.Query().Get("order")

	if sortBy == "" {
		sortBy = "name"
	}

	sort.Slice(files, func(i, j int) bool {
		a := files[i]
		b := files[j]

		if a.IsDir != b.IsDir {
			return a.IsDir
		}

		switch sortBy {
		case "type":
			if a.IsDir && b.IsDir {
				return compareStrings(a.Name, b.Name, order)
			}
			extA := strings.ToLower(a.Type)
			extB := strings.ToLower(b.Type)

			if extA == "" && extB != "" {
				return false
			}
			if extA != "" && extB == "" {
				return true
			}

			if extA != extB {
				return compareStrings(extA, extB, order)
			}

			return compareStrings(a.Name, b.Name, order)

		case "size":
			if a.Size != b.Size {
				if order == "desc" {
					return a.Size > b.Size
				}
				return a.Size < b.Size
			}
			return compareStrings(a.Name, b.Name, order)

		case "date":
			timeA, _ := time.Parse(time.RFC3339, a.ModTime)
			timeB, _ := time.Parse(time.RFC3339, b.ModTime)
			if !timeA.Equal(timeB) {
				if order == "desc" {
					return timeA.After(timeB)
				}
				return timeA.Before(timeB)
			}
			return compareStrings(a.Name, b.Name, order)
		default:
			return compareStrings(a.Name, b.Name, order)
		}
	})

	sendResponse(w, Response{Success: true, Content: files}, http.StatusOK)

}

func searchByNameHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		sendResponse(w, Response{Success: false, Error: "Method not allowed"}, http.StatusMethodNotAllowed)
		return
	}
	start := time.Now()
	defer logRequest(r, start)

	dirPath := r.URL.Query().Get("path")
	name := r.URL.Query().Get("name")

	if name == "" {
		sendResponse(w, Response{Success: false, Error: "Name is required"}, http.StatusBadRequest)
		return
	}

	absDirPath, err := pathGuard(rootDir, dirPath)

	if err != nil {
		sendResponse(w, Response{Success: false, Error: err.Error()}, http.StatusBadRequest)
		return
	}
	if isDir, err := isDir(absDirPath); err != nil {
		sendResponse(w, Response{Success: false, Error: err.Error()}, http.StatusInternalServerError)
		return
	} else if !existenceCheck(absDirPath) || !isDir {
		sendResponse(w, Response{Success: false, Error: "Directory not found"}, http.StatusBadRequest)
		return
	}

	results, err := searchFilesByName(absDirPath, name)
	if err != nil {
		logError(r, err)
		sendResponse(w, Response{Success: false, Error: err.Error()}, http.StatusInternalServerError)
		return
	}

	sendResponse(w, Response{Success: true, Content: results}, http.StatusOK)
}

func createFileHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		sendResponse(w, Response{Success: false, Error: "Method not allowed"}, http.StatusMethodNotAllowed)
		return
	}
	start := time.Now()
	defer logRequest(r, start)

	type request struct {
		Path    string `json:"path"`
		Rewrite bool   `json:"rewrite"`
	}

	var req request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sendResponse(w, Response{Success: false, Error: "Bad Request"}, http.StatusBadRequest)
	}

	// проверка безопасности пути источника
	absPath, err := pathGuard(rootDir, req.Path)
	if err != nil { // || absSrcPath == "" {
		sendResponse(w, Response{Success: false, Error: err.Error()}, http.StatusBadRequest)
		return
	}

	// проверка существования файла с таким же именем и значения флага перезаписи
	if existenceCheck(absPath) && !req.Rewrite {
		sendResponse(w, Response{Success: false, Error: "File with the same name already exists"}, http.StatusBadRequest)
		return
	}

	if _, err := os.Create(absPath); err != nil {
		logError(r, err)
		sendResponse(w, Response{Success: false, Error: err.Error()}, http.StatusInternalServerError)
		return
	}

	sendResponse(w, Response{Success: true}, http.StatusOK)

}

func createDirHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		sendResponse(w, Response{Success: false, Error: "Method not allowed"}, http.StatusMethodNotAllowed)
		return
	}
		start := time.Now()
	defer logRequest(r, start)

	// type request struct {
	// 	Path    string `json:"path"`
	// 	Rewrite bool   `json:"rewrite"`
	// }

	// var req request

	// if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
	// 	sendResponse(w, Response{Success: false, Error: "Bad Request"}, http.StatusBadRequest)
	// }

	path := r.URL.Query().Get("path")

	// проверка безопасности пути источника
	absPath, err := pathGuard(rootDir, path)
	if err != nil { // || absSrcPath == "" {
		sendResponse(w, Response{Success: false, Error: err.Error()}, http.StatusBadRequest)
		return
	}

	// проверка существования файла с таким же именем и значения флага перезаписи
	if existenceCheck(absPath) { //&& !req.Rewrite {
		sendResponse(w, Response{Success: false, Error: "Directory with the same name already exists"}, http.StatusBadRequest)
		return
	}

	if err := os.MkdirAll(absPath, 0755); err != nil {
		logError(r, err)
		sendResponse(w, Response{Success: false, Error: err.Error()}, http.StatusInternalServerError)
		return
	}

	sendResponse(w, Response{Success: true}, http.StatusOK)

}

func writeHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	start := time.Now()
	defer logRequest(r, start)

	type request struct {
		Path    string `json:"path"`
		Content string `json:"content"`
		Append  bool   `json:"append"`
	}

	var req request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sendResponse(w, Response{Success: false, Error: "Bad Request"}, http.StatusBadRequest)
		return
	}

	absPath, err := pathGuard(rootDir, req.Path)
	if err != nil {
		sendResponse(w, Response{Success: false, Error: err.Error()}, http.StatusBadRequest)
		return
	}

	if !existenceCheck(absPath) {
		sendResponse(w, Response{Success: false, Error: "File not found"}, http.StatusBadRequest)
		return
	}

	if err := write(absPath, req.Content, req.Append); err != nil {
		logError(r, err)
		sendResponse(w, Response{Success: false, Error: err.Error()}, http.StatusInternalServerError)
		return
	}

	sendResponse(w, Response{Success: true}, http.StatusOK)
}

func copyHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		sendResponse(w, Response{Success: false, Error: "Method not allowed"}, http.StatusMethodNotAllowed)
		return
	}
	start := time.Now()
	defer logRequest(r, start)

	type request struct {
		SrcPath string `json:"srcpath"`
		DstPath string `json:"dstpath"`
		Rewrite bool   `json:"rewrite"`
	}

	var req request

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sendResponse(w, Response{Success: false, Error: err.Error()}, http.StatusBadRequest)
		return
	}

	// проверка безопасности пути источника
	absSrcPath, err := pathGuard(rootDir, req.SrcPath)
	if err != nil { // || absSrcPath == "" {
		sendResponse(w, Response{Success: false, Error: err.Error()}, http.StatusBadRequest)
		return
	}

	//проверка отсутствия файла
	if !existenceCheck(absSrcPath) {
		sendResponse(w, Response{Success: false, Error: "Source file not found"}, http.StatusBadRequest)
		return
	}

	// проверка безопасности пути назначения
	absDstPath, err := pathGuard(rootDir, req.DstPath)
	if err != nil {
		sendResponse(w, Response{Success: false, Error: "Invalid path"}, http.StatusBadRequest)
		return
	}

	// проверка отсутствия вложенности путей src <- dst
	if sublevelCheck(absSrcPath, absDstPath) {
		sendResponse(w, Response{Success: false, Error: "Cannot copy a folder to itself"}, http.StatusBadRequest)
		return
	}

	// проверка существования файла и значения флага перезаписи
	if existenceCheck(absDstPath) && !req.Rewrite && absSrcPath != absDstPath {
		sendResponse(w, Response{Success: false, Error: "File with the same name already exists"}, http.StatusBadRequest)
		return
	}

	if err := copyRecursive(absSrcPath, absDstPath); err != nil {
		logError(r, err)
		sendResponse(w, Response{Success: false, Error: err.Error()}, http.StatusInternalServerError)
		return
	}

	sendResponse(w, Response{Success: true}, http.StatusOK)

}

func moveHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		sendResponse(w, Response{Success: false, Error: "Method not allowed"}, http.StatusMethodNotAllowed)
		return
	}
	start := time.Now()
	defer logRequest(r, start)

	type request struct {
		SrcPath string `json:"srcpath"`
		DstPath string `json:"dstpath"`
		Rewrite bool   `json:"rewrite"`
	}

	var req request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sendResponse(w, Response{Success: false, Error: err.Error()}, http.StatusBadRequest)
		return
	}

	absSrcPath, err := pathGuard(rootDir, req.SrcPath)
	if err != nil {
		sendResponse(w, Response{Success: false, Error: err.Error()}, http.StatusBadRequest)
		return
	}

	if !existenceCheck(absSrcPath) {
		sendResponse(w, Response{Success: false, Error: "Source file not found"}, http.StatusBadRequest)
		return
	}

	absDstPath, err := pathGuard(rootDir, req.DstPath)
	if err != nil {
		sendResponse(w, Response{Success: false, Error: "Invalid path"}, http.StatusBadRequest)
		return
	}

	// проверка отсутствия вложенности путей src <- dst
	if sublevelCheck(absSrcPath, absDstPath) {
		sendResponse(w, Response{Success: false, Error: "Cannot move a folder to itself"}, http.StatusBadRequest)
		return
	}

	// проверка существования файла и значения флага перезаписи
	if existenceCheck(absDstPath) && !req.Rewrite && absSrcPath != absDstPath {
		sendResponse(w, Response{Success: false, Error: "File with the same name already exists"}, http.StatusBadRequest)
		return
	}

	if err := move(absSrcPath, absDstPath); err != nil {
		logError(r, err)
		sendResponse(w, Response{Success: false, Error: err.Error()}, http.StatusInternalServerError)
		return
	}

	sendResponse(w, Response{Success: true}, http.StatusOK)

}

func getFileStatsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		sendResponse(w, Response{Success: false, Error: "Method not allowed"}, http.StatusMethodNotAllowed)
		return
	}
	start := time.Now()
	defer logRequest(r, start)

	path := r.URL.Query().Get("path")
	absPath, err := pathGuard(rootDir, path)
	if err != nil {
		sendResponse(w, Response{Success: false, Error: err.Error()}, http.StatusBadRequest)
		return
	}

	if !existenceCheck(absPath) {
		sendResponse(w, Response{Success: false, Error: "File not found"}, http.StatusBadRequest)
		return
	}

	if isDir, err := isDir(absPath); err != nil {
		sendResponse(w, Response{Success: false, Error: err.Error()}, http.StatusInternalServerError)
		return
	} else if isDir {
		sendResponse(w, Response{Success: false, Error: "Path is a directory"}, http.StatusBadRequest)
		return
	}

	stats, err := getFileStats(absPath)
	if err != nil {
		logError(r, err)
		sendResponse(w, Response{Success: false, Error: err.Error()}, http.StatusInternalServerError)
		return
	}

	sendResponse(w, Response{Success: true, Content: stats}, http.StatusOK)
}

func deleteHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		sendResponse(w, Response{Success: false, Error: "Method not allowed"}, http.StatusMethodNotAllowed)
		return
	}
	start := time.Now()
	defer logRequest(r, start)

	type request struct {
		Files []string `json:"files"`
	}

	var req request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sendResponse(w, Response{Success: false, Error: err.Error()}, http.StatusBadRequest)
		return
	}

	for _, relPath := range req.Files {
		absPath, err := pathGuard(rootDir, relPath)
		if err != nil {
			sendResponse(w, Response{Success: false, Error: err.Error()}, http.StatusBadRequest)
			return
		}

		if err := os.RemoveAll(absPath); err != nil {
			logError(r, err)
			sendResponse(w, Response{Success: false, Error: err.Error()}, http.StatusInternalServerError)
			return
		}
	}
	sendResponse(w, Response{Success: true}, http.StatusOK)
}

func main() {
	http.HandleFunc("/getinfo", getInfoHandler)
	http.HandleFunc("/getstats", getFileStatsHandler)
	http.HandleFunc("/getlist", getListHandler)
	http.HandleFunc("/read", readFileHandler)
	http.HandleFunc("/search", searchByNameHandler)
	http.HandleFunc("/createfile", createFileHandler)
	http.HandleFunc("/createdir", createDirHandler)
	http.HandleFunc("/write", writeHandler)
	http.HandleFunc("/copy", copyHandler)
	http.HandleFunc("/move", moveHandler)
	http.HandleFunc("/delete", deleteHandler)
	http.ListenAndServe(":10000", nil)

}
