package main

import (
	"net/http"

	"github.com/xyproto/pinterface"
)

func getUser(state pinterface.IUserState, r *http.Request) string {
	user := state.Username(r)
	// Only return username if it's not blank, user is confirmed and logged in
	if user != "" && state.IsConfirmed(user) && state.IsLoggedIn(user) && state.HasUser(user) {
		return user
	}
	return ""
}
