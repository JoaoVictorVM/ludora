package components

import (
	"strconv"
	"strings"
)

// FormatStars renders a star value using the Brazilian decimal comma, dropping
// the decimal part when it adds nothing ("4" instead of "4,0").
func FormatStars(stars float64) string {
	formatted := strconv.FormatFloat(stars, 'f', 1, 64)
	formatted = strings.TrimSuffix(formatted, ".0")

	return strings.Replace(formatted, ".", ",", 1)
}

func reviewCardID(reviewID int64) string {
	return "review-" + strconv.FormatInt(reviewID, 10)
}

func reviewControlsID(reviewID int64) string {
	return "review-controls-" + strconv.FormatInt(reviewID, 10)
}
