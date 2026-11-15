package main

import (
	"context"
	"crypto/subtle"
	"net/http"
	"strings"

	"gofr.dev/pkg/gofr"
)

// publicRoutes are called by the mobile app and carry no admin key.
func isPublic(r *http.Request) bool {
	if strings.HasPrefix(r.URL.Path, "/.well-known/") {
		return true
	}

	return r.Method == http.MethodPost && r.URL.Path == "/locations"
}

func adminKeyMiddleware(key string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if key == "" || isPublic(r) ||
				subtle.ConstantTimeCompare([]byte(r.Header.Get("X-API-Key")), []byte(key)) == 1 {
				next.ServeHTTP(w, r)
				return
			}

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":{"message":"missing or invalid X-API-Key"}}`))
		})
	}
}

func main() {
	app := gofr.New()

	s := &server{}
	fb, err := newFirebaseClient(context.Background(), app.Config.Get("FIREBASE_CREDENTIALS_FILE"), app.Config.Get("FIREBASE_PROJECT_ID"))
	s.fb, s.fbErr = fb, err
	if err != nil {
		app.Logger().Warnf("Firebase disabled: %v", err)
	} else {
		app.Logger().Infof("Firebase enabled for project %s", fb.projectID)
	}

	adminKey := app.Config.Get("ADMIN_API_KEY")
	if adminKey == "" {
		app.Logger().Warn("ADMIN_API_KEY is empty: read and notification endpoints are UNAUTHENTICATED")
	}
	app.UseMiddleware(adminKeyMiddleware(adminKey))

	app.OnStart(migrate)

	// Mobile app (public)
	app.POST("/locations", s.postLocations)

	// Admin (X-API-Key)
	app.GET("/locations/{employeeId}", s.getLocations)
	app.GET("/locations/{employeeId}/latest", s.getLatestLocation)
	app.DELETE("/locations/{employeeId}", s.deleteEmployeeLocations)
	app.DELETE("/locations", s.deleteAllLocations)
	app.GET("/employees", s.getEmployees)
	app.GET("/employees/count", s.getEmployeesCount)
	app.POST("/notifications", s.postNotification)

	app.Run()
}
