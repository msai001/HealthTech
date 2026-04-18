package main

import (
	"fmt"
	"health-app/internal/repository"
	"health-app/internal/service"
	"log"
	"net/http"
	"os" // Добавили этот импорт для работы с настройками сервера
)

const header = `
<!DOCTYPE html>
<html lang="ru">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <style>
        body { font-family: 'Segoe UI', sans-serif; background-color: #f0f2f5; margin: 0; padding: 20px; }
        .container { max-width: 600px; margin: 0 auto; background: white; padding: 25px; border-radius: 15px; box-shadow: 0 10px 25px rgba(0,0,0,0.1); }
        h2 { color: #1a73e8; border-bottom: 2px solid #e8f0fe; padding-bottom: 10px; }
        .search-box { display: flex; gap: 10px; margin-bottom: 20px; }
        input[type="text"] { flex-grow: 1; padding: 12px; border: 1px solid #ddd; border-radius: 8px; outline: none; }
        button { padding: 12px 18px; border: none; border-radius: 8px; cursor: pointer; font-weight: 600; transition: 0.2s; }
        .btn-add { background-color: #1a73e8; color: white; }
        .btn-del { background-color: #d93025; color: white; padding: 8px 12px; }
        .btn-edit { background-color: #fbbc04; color: #3c4043; padding: 8px 12px; text-decoration: none; border-radius: 8px; font-size: 14px; margin-right: 5px; }
        ul { list-style: none; padding: 0; }
        li { background: #fff; margin-bottom: 12px; padding: 15px; border-radius: 10px; border: 1px solid #eee; display: flex; justify-content: space-between; align-items: center; }
        a.back-link { display: inline-block; margin-top: 20px; color: #1a73e8; text-decoration: none; font-weight: bold; }
    </style>
</head>
<body>
<div class="container">
`
const footer = `</div></body></html>`

func main() {
	// ТУТ МАГИЯ: если есть настройка DB_URL, берем её. Если нет — используем локалку.
	connStr := os.Getenv("DB_URL")
	if connStr == "" {
		connStr = "host=127.0.0.1 port=5432 user=postgres password=atyrau2026 dbname=postgres sslmode=disable"
	}

	repo, err := repository.NewPostgresRepo(connStr)
	if err != nil {
		log.Fatal(err)
	}
	svc := service.NewHealthService(repo)

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, header+`
			<h2>📝 Запись пациента</h2>
			<form action="/create" method="POST">
				<input type="text" name="patient_name" placeholder="Введите имя" required>
				<button type="submit" class="btn-add">Добавить</button>
			</form>
			<br><br>
			<a href="/appointments" class="back-link">📂 Перейти к списку записей →</a>
		`+footer)
	})

	http.HandleFunc("/appointments", func(w http.ResponseWriter, r *http.Request) {
		search := r.URL.Query().Get("search")
		list, err := svc.GetList(search)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, header+`<h2>📋 Список записей</h2>
			<form method="GET" class="search-box">
				<input type="text" name="search" placeholder="Поиск по имени..." value="`+search+`" style="flex-grow: 1;">
				<button type="submit" class="btn-add">Найти</button>
				<a href="/appointments" style="align-self:center; font-size:14px;">Сброс</a>
			</form><ul>`)

		for _, item := range list {
			fmt.Fprintf(w, `
				<li>
					<span><strong>%s</strong></span>
					<div class="actions">
						<a href="/edit?id=%d&name=%s" class="btn-edit">Изменить</a>
						<form action="/delete" method="POST" style="margin:0; display:inline;">
							<input type="hidden" name="id" value="%d">
							<button type="submit" class="btn-del">Удалить</button>
						</form>
					</div>
				</li>`, item.PatientName, item.ID, item.PatientName, item.ID)
		}
		fmt.Fprint(w, "</ul><a href='/' class='back-link'>⬅ Назад</a>"+footer)
	})

	http.HandleFunc("/edit", func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Query().Get("id")
		name := r.URL.Query().Get("name")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, header)
		fmt.Fprintf(w, `
			<h2>✏️ Изменить имя</h2>
			<form action="/update" method="POST">
				<input type="hidden" name="id" value="%s">
				<input type="text" name="new_name" value="%s" required>
				<button type="submit" class="btn-add">Сохранить</button>
			</form>
			<br><a href="/appointments" class="back-link">Отмена</a>
		`, id, name)
		fmt.Fprint(w, footer)
	})

	http.HandleFunc("/update", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			svc.Update(r.FormValue("id"), r.FormValue("new_name"))
			http.Redirect(w, r, "/appointments", http.StatusSeeOther)
		}
	})

	http.HandleFunc("/create", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			svc.Create(r.FormValue("patient_name"))
			http.Redirect(w, r, "/appointments", http.StatusSeeOther)
		}
	})

	http.HandleFunc("/delete", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			svc.Delete(r.FormValue("id"))
			http.Redirect(w, r, "/appointments", http.StatusSeeOther)
		}
	})

	log.Println("Сервер готов к деплою: http://localhost:8080")
	http.ListenAndServe(":8080", nil)
}
