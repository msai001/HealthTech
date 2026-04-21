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

var googleOAuthConfig = &oauth2.Config{
ClientID:     os.Getenv("GOOGLE_CLIENT_ID"),
ClientSecret: os.Getenv("GOOGLE_CLIENT_SECRET"),
RedirectURL:  "https://healthtech-1.onrender.com/callback",
Scopes:       []string{"openid", "email", "profile"},
Endpoint:     google.Endpoint,
}

var db *sql.DB

// JSON Responses
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

// Middleware для CORS
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

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
w.WriteHeader(status)
json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, message string) {
writeJSON(w, status, ErrorResponse{Error: message})
}

func initDB() {
connStr := os.Getenv("DATABASE_URL")
var err error
db, err = sql.Open("postgres", connStr)
if err != nil {
log.Fatal(err)
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

func main() {
initDB()
rand.Seed(time.Now().UnixNano())

// API Routes
http.HandleFunc("/api/auth/callback", corsMiddleware(handleOAuthCallback))
http.HandleFunc("/api/auth/verify-otp", corsMiddleware(handleOTPVerify))
http.HandleFunc("/api/auth/me", corsMiddleware(handleGetCurrentUser))
http.HandleFunc("/api/auth/logout", corsMiddleware(handleLogout))

http.HandleFunc("/api/appointments", corsMiddleware(handleAppointmentsAPI))
http.HandleFunc("/api/appointments/", corsMiddleware(handleAppointmentAPI))

// Legacy routes
http.HandleFunc("/", corsMiddleware(handleLegacyRoot))

port := os.Getenv("PORT")
if port == "" {
port = "8080"
}
log.Printf("Server running on port %s", port)
log.Fatal(http.ListenAndServe(":"+port, nil))
}

// OAuth Callback
func handleOAuthCallback(w http.ResponseWriter, r *http.Request) {
code := r.URL.Query().Get("code")
if code == "" {
writeError(w, http.StatusBadRequest, "Missing authorization code")
return
}

token, err := googleOAuthConfig.Exchange(context.Background(), code)
if err != nil {
writeError(w, http.StatusUnauthorized, "Failed to exchange token")
return
}

client := googleOAuthConfig.Client(context.Background(), token)
resp, err := client.Get("https://www.googleapis.com/oauth2/v2/userinfo")
if err != nil {
writeError(w, http.StatusInternalServerError, "Failed to get user info")
return
}
defer resp.Body.Close()

var user struct{ Email string }
if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
writeError(w, http.StatusInternalServerError, "Failed to parse user info")
return
}

code = fmt.Sprintf("%06d", rand.Intn(1000000))
db.Exec("DELETE FROM appointments WHERE user_email = $1 AND doctor_name = 'System'", user.Email)
db.Exec("INSERT INTO appointments (user_email, totp_secret, doctor_name, patient_name, appointment_date) VALUES ($1, $2, 'System', 'User', '2026-01-01')", user.Email, code)

go sendMail(user.Email, "HealthTech Code", "Ваш код для входа: "+code)

http.SetCookie(w, &http.Cookie{Name: "user_email", Value: user.Email, Path: "/", MaxAge: 86400, HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})

writeJSON(w, http.StatusOK, SuccessResponse{
Message: "OTP sent to email",
Data: map[string]string{
"email": user.Email,
},
})
}

// OTP Verify
func handleOTPVerify(w http.ResponseWriter, r *http.Request) {
if r.Method != http.MethodPost {
writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
return
}

c, err := r.Cookie("user_email")
if err != nil {
writeError(w, http.StatusUnauthorized, "Missing user_email cookie")
return
}

var req struct {
Code string `json:"code"`
}
if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
writeError(w, http.StatusBadRequest, "Invalid request")
return
}

var saved string
err = db.QueryRow("SELECT totp_secret FROM appointments WHERE user_email = $1 AND doctor_name = 'System' ORDER BY id DESC LIMIT 1", c.Value).Scan(&saved)

if err != nil || strings.TrimSpace(req.Code) != saved || saved == "" {
writeError(w, http.StatusUnauthorized, "Invalid OTP code")
return
}

http.SetCookie(w, &http.Cookie{Name: "session_valid", Value: "true", Path: "/", MaxAge: 86400, HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})

writeJSON(w, http.StatusOK, UserResponse{Email: c.Value})
}

// Get Current User
func handleGetCurrentUser(w http.ResponseWriter, r *http.Request) {
c, err := r.Cookie("user_email")
if err != nil {
writeError(w, http.StatusUnauthorized, "Not authenticated")
return
}

writeJSON(w, http.StatusOK, UserResponse{Email: c.Value})
}

// Logout
func handleLogout(w http.ResponseWriter, r *http.Request) {
http.SetCookie(w, &http.Cookie{Name: "session_valid", Value: "", Path: "/", MaxAge: -1})
writeJSON(w, http.StatusOK, SuccessResponse{Message: "Logged out"})
}

// Get/Create/Delete Appointments
func handleAppointmentsAPI(w http.ResponseWriter, r *http.Request) {
cEmail, err := r.Cookie("user_email")
if err != nil {
writeError(w, http.StatusUnauthorized, "Not authenticated")
return
}

switch r.Method {
case http.MethodGet:
rows, err := db.Query("SELECT id, doctor_name, appointment_date FROM appointments WHERE user_email = $1 AND doctor_name != 'System' ORDER BY id DESC", cEmail.Value)
if err != nil {
writeError(w, http.StatusInternalServerError, "Database error")
return
}
defer rows.Close()

var appointments []AppointmentResponse
for rows.Next() {
var a AppointmentResponse
rows.Scan(&a.ID, &a.DoctorName, &a.AppointmentDate)
appointments = append(appointments, a)
}

writeJSON(w, http.StatusOK, map[string]interface{}{
"appointments": appointments,
})

case http.MethodPost:
var req struct {
DoctorName      string `json:"doctor_name"`
AppointmentDate string `json:"appointment_date"`
}
if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
writeError(w, http.StatusBadRequest, "Invalid request")
return
}

var id int
err := db.QueryRow("INSERT INTO appointments (user_email, doctor_name, appointment_date, patient_name, totp_secret) VALUES ($1, $2, $3, 'Patient', '') RETURNING id", cEmail.Value, req.DoctorName, req.AppointmentDate).Scan(&id)
if err != nil {
writeError(w, http.StatusInternalServerError, "Failed to create appointment")
return
}

appointment := AppointmentResponse{
ID:              id,
DoctorName:      req.DoctorName,
AppointmentDate: req.AppointmentDate,
PatientName:     "Patient",
}

writeJSON(w, http.StatusCreated, SuccessResponse{
Message: "Appointment created",
Data: map[string]interface{}{
"appointment": appointment,
},
})

default:
writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
}
}

// Delete Appointment
func handleAppointmentAPI(w http.ResponseWriter, r *http.Request) {
if r.Method != http.MethodDelete {
writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
return
}

cEmail, err := r.Cookie("user_email")
if err != nil {
writeError(w, http.StatusUnauthorized, "Not authenticated")
return
}

id := strings.TrimPrefix(r.URL.Path, "/api/appointments/")

_, err = db.Exec("DELETE FROM appointments WHERE id = $1 AND user_email = $2", id, cEmail.Value)
if err != nil {
writeError(w, http.StatusInternalServerError, "Failed to delete appointment")
return
}

writeJSON(w, http.StatusOK, SuccessResponse{Message: "Appointment deleted"})
}

// Legacy endpoint
func handleLegacyRoot(w http.ResponseWriter, r *http.Request) {
writeJSON(w, http.StatusOK, map[string]string{
"message": "Use /api endpoints for the JSON API",
})
}
