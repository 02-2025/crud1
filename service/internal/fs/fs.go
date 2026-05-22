package fs

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"
	"sort"
)

// ROOT_DIR переменная окружения корневой директории в контейнере docker
var RootDir = os.Getenv("ROOT_DIR")

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

// проверка вложенности пути src в dst
// используется для предотвращения копирования/перемещения директории в себя же
func SublevelCheck(src, dst string) bool {
	rel, err := filepath.Rel(src, dst)
	if err != nil || rel == "." {
		return false
	}
	return !strings.HasPrefix(rel, "..")
}

// файл по пути path существует: true;
// файл не существует: false
func ExistenceCheck(path string) bool {
	_, err := os.Stat(path)
	return !os.IsNotExist(err)
}

// файл по пути path — директория: true;
// файл не директория: false
func IsDir(path string) (bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		return false, err
	}

	return info.IsDir(), nil
}

// функция приводит строковый путь path в безопасную форму, и возвращает абсолютный путь,
// полученный путём присоединения корневой директории root;
// полученный путь исключает возможность выхода за пределы корневой директории;
// функция требует, чтобы в качестве аргумента path был передан абсолютный путь (начинающийся с /),
// иначе будет возвращена ошибка
func PathGuard(root, path string) (string, error) {
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

// вспомогательная функция заполнения структуры FileInfo — метаданных файла
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

// функция чтения текстового файла по его пути, возвращает строку с содержимым файла
func Read(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// функция получения метаданных файла по его пути
func GetFileInfoByPath(path string) (FileInfo, error) {
	file, err := os.Stat(path)
	if err != nil {
		return FileInfo{}, err
	}
	fileInfo, err := getFileInfo(fileAd{file})
	if err != nil {
		return FileInfo{}, err
	}
	return fileInfo, nil
}

// функция получения списка файлов внутри директории по её пути, с возможностью сортировки по имени, типу, размеру, дате изменения
func GetFilesList(path, sortBy, order string) ([]FileInfo, error) {
	dirList, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	var files []FileInfo
	for _, entry := range dirList {
		file, err := getFileInfo(entry)
		if err != nil {
			return nil, err
		}
		files = append(files, file)
	}

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
	return files, nil
}

// функция подсчёта статистики текстового файла:
// слова, уникальные слова, символы, строки
func GetFileStats(path string) (FileStats, error) {
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
func SearchFilesByName(root string, query string) ([]FileInfo, error) {
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

// функция создания файла
func CreateFile(absPath string) error {
	_, err := os.Create(absPath)
	return err
}

// функция создания директории
func CreateDir(absPath string) error {
	return os.MkdirAll(absPath, 0755)
}

// копирование файла с src путём, по пути dst, включая вложенные файлы, если источник директория
func CopyRecursive(src, dst string) error {
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
				if err := CopyRecursive(srcPath, dstPath); err != nil {
					return err
				}
			} else {
				if err := CopyFile(srcPath, dstPath, append); err != nil {
					return err
				}
			}
		}
	} else {
		if err := CopyFile(src, dst, append); err != nil {
			return err
		}
	}
	return nil
}

// копирование файла путём открытия для чтения источника src, создания файла назначения dst
// и копирования содержимого из файла с src путём в файл с dst путём;
func CopyFile(src, dst, append string) error {
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

	for ExistenceCheck(dst+append) && append != "" {
		append += tempAppend
	}

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
func Move(src, dst string) error {

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

	if err := CopyRecursiveForMove(src, dst); err != nil {
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
func CopyRecursiveForMove(src, dst string) error {
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
				if err := CopyRecursiveForMove(srcPath, dstPath); err != nil {
					return err
				}
			} else {
				if err := CopyFileForMove(srcPath, dstPath); err != nil {
					return err
				}
			}
		}
	} else {
		if err := CopyFileForMove(src, dst); err != nil {
			return err
		}
	}

	return nil
}

// копирование файла путём открытия для чтения источника src, создания файла назначения dst
// и копирования содержимого из src в dst;
// является вспомогательной для copyRecursiveForMove()
// отличается от copyFile() отсутствием логики для добавления строки в конец имени файла
func CopyFileForMove(src, dst string) error {
	from, err := os.Open(src)
	if err != nil {
		return err
	}
	defer from.Close()

	srcInfo, err := from.Stat()
	if err != nil {
		return err
	}

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
func Write(absPath string, content string, append bool) error {
	origInfo, err := os.Stat(absPath)
	if err != nil {
		return err
	}

	if append {
		file, err := os.OpenFile(absPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, origInfo.Mode().Perm())
		if err != nil {
			return err
		}
		defer file.Close()

		if _, err := file.WriteString(content); err != nil {
			return err
		}

	} else {
		tempFile, err := os.CreateTemp(filepath.Dir(absPath), "."+filepath.Base(absPath)+".temp-*")
		if err != nil {
			return err
		}

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

func Delete(files []string) error {
	for _, relPath := range files {
		absPath, err := PathGuard(RootDir, relPath)
		if err != nil {
			return err
		}
		if err := os.RemoveAll(absPath); err != nil {
			return err
		}
	}
	return nil
}
