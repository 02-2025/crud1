package main

import (
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
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
	IsDir   bool   `json:"isdir"`
	Name    string `json:"name"`
	Size    int64  `json:"size"`
	ModTime string `json:"mod_time"`
}

// интерфейс, который объединяет сигнатуры fs.FileInfo и os.DirEntry интерфейсов;
// используется для правильной обработки выше упомянутых интерфейсов в функции getFileInfo
type file interface {
	Info() (fs.FileInfo, error)
	Name() string
	IsDir() bool
}

// структура, которая реализует интерфейс file
type fileAd struct {
	fs.FileInfo
}

// реализация метода интерейса file, для его работы;
// возвращает интерфейс fs.FileInfo, описывающий файл
func (f fileAd) Info() (fs.FileInfo, error) {
	return f.FileInfo, nil
}

// функция для заполнения структуры FileInfo для отправки в ответе на запрос
func getFileInfo(file file) (FileInfo, error) {
	info, err := file.Info()
	if err != nil {
		return FileInfo{}, err
	}

	return FileInfo{
		IsDir:   file.IsDir(),
		Name:    file.Name(),
		Size:    info.Size(),
		ModTime: info.ModTime().String(),
	}, nil
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
func write(absPath string, content string) error {
	origInfo, err := os.Stat(absPath)
	if err != nil {
		return err
	}

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

	return nil
}

// ОБРАБОТЧИКИ HTTP ЗАПРОСОВ
func readFileHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		sendResponse(w, Response{Success: false, Error: "Method not allowed"}, http.StatusMethodNotAllowed)
		return
	}

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
			sendResponse(w, Response{Success: false, Error: err.Error()}, http.StatusInternalServerError)
			return
		}
		files = append(files, file)
	}

	sendResponse(w, Response{Success: true, Content: files}, http.StatusOK)

}

func createFileHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		sendResponse(w, Response{Success: false, Error: "Method not allowed"}, http.StatusMethodNotAllowed)
		return
	}

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

	type request struct {
		Path    string `json:"path"`
		Content string `json:"content"`
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

	if err := write(absPath, req.Content); err != nil {
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
		sendResponse(w, Response{Success: false, Error: err.Error()}, http.StatusInternalServerError)
		return
	}

	sendResponse(w, Response{Success: true}, http.StatusOK)

}

func deleteHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		sendResponse(w, Response{Success: false, Error: "Method not allowed"}, http.StatusMethodNotAllowed)
		return
	}

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
			sendResponse(w, Response{Success: false, Error: err.Error()}, http.StatusInternalServerError)
			return
		}
	}
	sendResponse(w, Response{Success: true}, http.StatusOK)
}

func main() {
	http.HandleFunc("/getinfo", getInfoHandler)
	http.HandleFunc("/getlist", getListHandler)
	http.HandleFunc("/read", readFileHandler)
	http.HandleFunc("/createfile", createFileHandler)
	http.HandleFunc("/createdir", createDirHandler)
	http.HandleFunc("/write", writeHandler)
	http.HandleFunc("/copy", copyHandler)
	http.HandleFunc("/move", moveHandler)
	http.HandleFunc("/delete", deleteHandler)
	http.ListenAndServe(":10000", nil)

}
