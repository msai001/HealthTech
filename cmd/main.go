package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"net/smtp"
	"os"
	"strings"
	"time"

	_ "github.com/lib/pq"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// --- CONFIGURATION ---
var googleOAuthConfig = &oauth2.Config{
	ClientID:     os.Getenv("GOOGLE_CLIENT_ID"),
	ClientSecret: os.Getenv("GOOGLE_CLIENT_SECRET"),
	RedirectURL:  "https://healthtech-1.onrender.com/api/auth/callback",
	Scopes:       []string{"openid", "email", "profile"},
	Endpoint:     google.Endpoint,
}

var db *sql.DB

// --- MODELS ---
type ErrorResponse struct {
	Error string `json:"error"`
}

type SuccessResponse struct {
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

type AppointmentResponse struct {
	ID              int    `json:"id"`
	DoctorName      string `json:"doctor_name"`
	AppointmentDate string `json:"appointment_date"`
	PatientName     string `json:"patient_name"`
}

type UserResponse struct {
	Email string `json:"email"`
}

// --- MIDDLEWARE ---
func corsMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}
		next(w, r)
	}
}

// --- HELPERS ---
func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, message string) {
	log.Printf("ERROR: %s", message)
	writeJSON(w, status, ErrorResponse{Error: message})
}

func initDB() {
	connStr := os.Getenv("DATABASE_URL")
	var err error
	db, err = sql.Open("postgres", connStr)
	if err != nil {
		log.Fatal("Database connection failed: ", err)
	}
}

func sendMail(to, subject, body string) {
	from, pass := os.Getenv("EMAIL_USER"), os.Getenv("EMAIL_PASS")
	if from == "" || pass == "" {
		return
	}
	auth := smtp.PlainAuth("", from, pass, "smtp.gmail.com")
	msg := []byte("Subject: " + subject + "\r\n\r\n" + body)
	_ = smtp.SendMail("smtp.gmail.com:587", auth, from, []string{to}, msg)
}

// --- MAIN ---
func main() {
	initDB()
	rand.Seed(time.Now().UnixNano())

	// Инициация входа через Google
	http.HandleFunc("/api/auth/google", func(w http.ResponseWriter, r *http.Request) {
		url := googleOAuthConfig.AuthCodeURL("state")
		http.Redirect(w, r, url, http.StatusTemporaryRedirect)
	})

	// Auth Endpoints
	http.HandleFunc("/api/auth/callback", corsMiddleware(handleOAuthCallback))
	http.HandleFunc("/api/auth/verify-otp", corsMiddleware(handleOTPVerify))
	http.HandleFunc("/api/auth/me", corsMiddleware(handleGetCurrentUser))
	http.HandleFunc("/api/auth/logout", corsMiddleware(handleLogout))

	// Data Endpoints
	http.HandleFunc("/api/appointments", corsMiddleware(handleAppointmentsAPI))
	http.HandleFunc("/api/appointments/", corsMiddleware(handleAppointmentAPI))

	// Root Interface (HTML)
	http.HandleFunc("/", handleLegacyRoot)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Printf("Server Version 1.5.3 running on port %s", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

// --- HANDLERS ---

func handleOAuthCallback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	if code == "" {
		writeError(w, http.StatusBadRequest, "Code missing")
		return
	}

	token, err := googleOAuthConfig.Exchange(context.Background(), code)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "Exchange failed")
		return
	}

	client := googleOAuthConfig.Client(context.Background(), token)
	resp, err := client.Get("https://www.googleapis.com/oauth2/v2/userinfo")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "User info error")
		return
	}
	defer resp.Body.Close()

	var user struct{ Email string }
	json.NewDecoder(resp.Body).Decode(&user)

	otp := fmt.Sprintf("%06d", rand.Intn(1000000))
	db.Exec("DELETE FROM appointments WHERE user_email = $1 AND doctor_name = 'System'", user.Email)
	db.Exec("INSERT INTO appointments (user_email, totp_secret, doctor_name, patient_name, appointment_date) VALUES ($1, $2, 'System', 'User', '2026-01-01')", user.Email, otp)

	go sendMail(user.Email, "Login Code", "Your code: "+otp)

	http.SetCookie(w, &http.Cookie{
		Name: "user_email", Value: user.Email, Path: "/", MaxAge: 86400,
		HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode,
	})

	// Возвращаем пользователя на главную страницу
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func handleOTPVerify(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Use POST")
		return
	}

	c, err := r.Cookie("user_email")
	if err != nil {
		writeError(w, http.StatusUnauthorized, "No cookie")
		return
	}

	var req struct {
		Code string `json:"code"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	var savedOtp string
	err = db.QueryRow("SELECT totp_secret FROM appointments WHERE user_email = $1 AND doctor_name = 'System' ORDER BY id DESC LIMIT 1", c.Value).Scan(&savedOtp)

	if err != nil || strings.TrimSpace(req.Code) != savedOtp || savedOtp == "" {
		writeError(w, http.StatusUnauthorized, "Invalid OTP")
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name: "session_valid", Value: "true", Path: "/", MaxAge: 86400,
		HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode,
	})

	writeJSON(w, http.StatusOK, UserResponse{Email: c.Value})
}

func handleAppointmentsAPI(w http.ResponseWriter, r *http.Request) {
	cEmail, err := r.Cookie("user_email")
	if err != nil {
		writeError(w, http.StatusUnauthorized, "Login required")
		return
	}

	switch r.Method {
	case http.MethodGet:
		rows, err := db.Query("SELECT id, doctor_name, appointment_date FROM appointments WHERE user_email = $1 AND doctor_name != 'System' ORDER BY id DESC", cEmail.Value)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "DB Error")
			return
		}
		defer rows.Close()

		var appointments []AppointmentResponse
		for rows.Next() {
			var a AppointmentResponse
			if err := rows.Scan(&a.ID, &a.DoctorName, &a.AppointmentDate); err == nil {
				appointments = append(appointments, a)
			}
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"appointments": appointments})

	case http.MethodPost:
		var req struct {
			DoctorName string `json:"doctor_name"`
			Date       string `json:"appointment_date"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "Invalid JSON")
			return
		}

		_, err := db.Exec("INSERT INTO appointments (user_email, doctor_name, appointment_date, patient_name, totp_secret) VALUES ($1, $2, $3, 'Patient', '')", cEmail.Value, req.DoctorName, req.Date)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "Failed to save")
			return
		}
		writeJSON(w, http.StatusCreated, SuccessResponse{Message: "Success"})

	default:
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

func handleAppointmentAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		writeError(w, http.StatusMethodNotAllowed, "Use DELETE")
		return
	}

	cEmail, err := r.Cookie("user_email")
	if err != nil {
		writeError(w, http.StatusUnauthorized, "Unauthenticated")
		return
	}

	id := strings.TrimPrefix(r.URL.Path, "/api/appointments/")
	_, err = db.Exec("DELETE FROM appointments WHERE id = $1 AND user_email = $2", id, cEmail.Value)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Delete failed")
		return
	}

	writeJSON(w, http.StatusOK, SuccessResponse{Message: "Deleted"})
}

func handleGetCurrentUser(w http.ResponseWriter, r *http.Request) {
	c, err := r.Cookie("user_email")
	if err != nil {
		writeError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}
	writeJSON(w, http.StatusOK, UserResponse{Email: c.Value})
}

func handleLogout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: "session_valid", Value: "", Path: "/", MaxAge: -1})
	writeJSON(w, http.StatusOK, SuccessResponse{Message: "Logged out"})
}

func handleLegacyRoot(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, `
		<!DOCTYPE html>
		<html lang="ru">
		<head>
			<meta charset="UTF-8">
			<title>Health Monitoring System</title>
			<style>
				body { font-family: 'Segoe UI', Tahoma, Geneva, Verdana, sans-serif; background: #f4f7f6; display: flex; justify-content: center; align-items: center; height: 100vh; margin: 0; }
				.card { background: white; padding: 40px; border-radius: 12px; box-shadow: 0 10px 25px rgba(0,0,0,0.1); text-align: center; max-width: 400px; width: 100%; }
				h1 { color: #2c3e50; margin-bottom: 10px; }
				p { color: #7f8c8d; margin-bottom: 30px; }
				.btn-google { background: #4285F4; color: white; padding: 12px 24px; text-decoration: none; border-radius: 6px; font-weight: bold; display: inline-flex; align-items: center; transition: background 0.3s; }
				.btn-google:hover { background: #357ae8; }
				.status { margin-top: 20px; font-size: 0.8em; color: #bdc3c7; }
			</style>
		</head>
		<body>
			<div class="card">
				<h1>Health Monitoring</h1>
				<p>Добро пожаловать в систему мониторинга здоровья.</p>
				<a href="/api/auth/google" class="btn-google">Войти через Google</a>
				<div class="status">Backend v1.5.3 | Status: Running</div>
			</div>
		</body>
		</html>
	`)
}
