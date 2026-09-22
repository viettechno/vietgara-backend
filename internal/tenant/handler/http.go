package handler

import "github.com/labstack/echo/v4"

// RegisterHTTPRoutes adds the browser-facing staff and garage endpoints to Echo.
func (s *Server) RegisterHTTPRoutes(router *echo.Group) {
	s.registerGarageHTTPRoutes(router)
	s.registerStaffHTTPRoutes(router)
}
