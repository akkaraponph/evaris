package render

import (
	"encoding/json"
	"github.com/akkaraponph/evaris/internal/domain"
)

func JSON(r domain.Report) ([]byte, error) {
	b, e := json.MarshalIndent(r, "", "  ")
	return append(b, '\n'), e
}
