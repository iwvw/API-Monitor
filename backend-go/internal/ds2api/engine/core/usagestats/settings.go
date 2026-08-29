package usagestats

type ModelPrice struct {
	InputPrice  float64 `json:"input_price"`
	OutputPrice float64 `json:"output_price"`
}

type PeakSettings struct {
	Enabled       bool    `json:"enabled"`
	Start1        string  `json:"start1"`
	End1          string  `json:"end1"`
	Start2        string  `json:"start2"`
	End2          string  `json:"end2"`
	Multiplier    float64 `json:"multiplier"`
	WeekendNormal bool    `json:"weekend_normal"`
}

type UsageSettings struct {
	Enabled bool                  `json:"enabled"`
	Models  map[string]ModelPrice `json:"models"`
	Peak    PeakSettings          `json:"peak"`
}

func DefaultSettings() UsageSettings {
	return UsageSettings{
		Enabled: true,
		Models: map[string]ModelPrice{
			"v4-pro": {InputPrice: 0.15, OutputPrice: 13.5},
			"vision": {InputPrice: 0.05, OutputPrice: 4.5},
			"flash":  {InputPrice: 0.05, OutputPrice: 4.5},
		},
		Peak: PeakSettings{
			Enabled:       true,
			Start1:        "09:00",
			End1:          "12:00",
			Start2:        "14:00",
			End2:          "18:00",
			Multiplier:    2,
			WeekendNormal: true,
		},
	}
}
