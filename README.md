# Микросервис для работы с файлами

Микросервис на Golang для работы с файловой системой Linux:
- чтение
- запись
- создание
- копирование
- перемещение
- удаление



# Запуск

```bash
docker compose up --build -d
```
адрес сервиса: http://localhost:10000

# Переменные окружения докера

`ROOT_DIR` — имя директории внутри контекста контейнера, которая будет считаться корневой, изначально это "data".

# Эндпоинты

| эндпоинт                         | необходимые поля          | функция                        |
| -------------------------------- | ------------------------- | ------------------------------ |
| `GET /getinfo?path=/file`        |                           | получить информацию о файле    |
| `GET /getlist?path=/folder`      |                           | получить список файлов и папок |
| `GET /read?path=/file`           |                           | получить содержимое файла      |
|                                  |                           |                                |
| `PUT /createfile`                | path, rewrite             | создать пустой файл            |
| `PUT /createdir?path=/newfolder` |                           | создать директорию             |
| `PUT /write`                     | path, content             | записать в файл                |
|                                  |                           |                                |
| `POST /copy`                     | srcpath, dstpath, rewrite | скопировать файл               |
| `POST /move`                     | srcpath, dstpath, rewrite | переместить файл               |
|                                  |                           |                                |
| `DELETE /delete`                 | files                     | удалить файл                   |

# Примеры curl запросов

##### с query параметрами
**получение списка файлов в директории**
```bash
curl -X GET http://localhost:10000/getlist?path=/a/
```
**получение данных о файле**
```bash
curl -X GET http://localhost:10000/fileinfo?path=/file
```

##### с application/json
**запись в файл**
```bash
curl -X PUT http://localhost:10000/write -H "Content-Type: application/json" -d '{"path":"/file","content":"hello world!"}'
```

**копирование**
```bash
curl -X POST http://localhost:10000/copy -H "Content-Type: application/json" -d '{"srcpath":"/file1","dstpath":"/a/file1 — copy","rewrite":false}'
```

**удаление**
```bash
curl -X DELETE http://localhost:10000/delete -H "Content-Type: application/json" -d '{"files": ["/file1", "/a/file1 — copy", "/a/b/"]}'
```
