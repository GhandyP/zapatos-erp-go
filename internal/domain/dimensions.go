package domain

import "fmt"

type Dimensions struct {
	LengthCM float64 `json:"lengthCm"`
	WidthCM  float64 `json:"widthCm"`
	HeightCM float64 `json:"heightCm"`
	WeightKG float64 `json:"weightKg"`
}

func (d Dimensions) Validate() error {
	if d.LengthCM <= 0 || d.WidthCM <= 0 || d.HeightCM <= 0 {
		return ValidationError{Message: "dimensions require positive lengthCm, widthCm and heightCm"}
	}
	if d.WeightKG < 0 {
		return ValidationError{Message: "weightKg cannot be negative"}
	}
	return nil
}

func (d Dimensions) String() string {
	return fmt.Sprintf("%.2f x %.2f x %.2f cm, %.2f kg", d.LengthCM, d.WidthCM, d.HeightCM, d.WeightKG)
}
