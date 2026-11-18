// Package dto описывает данные HTTP-запросов и ответов API.
package dto

// SuccessResponse — стандартный ответ для операций без полезной нагрузки.
type SuccessResponse struct {
	Message string `json:"message"`
}
