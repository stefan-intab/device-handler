package soaptx

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"strconv"
	"strings"

	"device-handler/internal/device"
)

type Parser struct {
	model string
}

func New(model string) *Parser {
	return &Parser{model: model}
}

func SupportedModels() []string {
	return []string{
		"T3511",
		"T4511",
		"T7511",
		"T2514",
		"T0510",
		"T3510",
		"T7510",
		"T0610",
		"T4611",
		"T3610",
		"T3611",
		"T7610",
		"T7611",
		"T7613D",
		"T5540",
		"T6540",
		"T5541",
		"T6541",
		"T6640",
		"T5640",
		"T5545",
		"T6545",
		"T5641",
		"T6641",
	}
}

func (p *Parser) Manufacturer() string { return "comet" }
func (p *Parser) Model() string        { return p.model }

func (p *Parser) Parse(_ context.Context, req device.Request) (device.ParseResult, error) {
	body := bytes.TrimPrefix(req.Body, []byte("\xef\xbb\xbf"))

	var envelope envelope
	if err := xml.Unmarshal(body, &envelope); err != nil {
		return device.ParseResult{}, fmt.Errorf("decode comet soap payload: %w", err)
	}

	var parsed parseResult
	switch {
	case envelope.Body.InsertTx5xxSample != nil:
		result, err := parseBaseSample(envelope.Body.InsertTx5xxSample.baseSample)
		if err != nil {
			return device.ParseResult{}, err
		}
		if pressure, ok, err := parseOptionalFloat(envelope.Body.InsertTx5xxSample.Pressure, -9999); err != nil {
			return device.ParseResult{}, fmt.Errorf("read pressure: %w", err)
		} else if ok {
			result.channels = append(result.channels, device.ChannelMeasurement{
				Name:  "Pressure",
				Unit:  normalizePressureUnit(envelope.Body.InsertTx5xxSample.PressureU),
				Value: pressure,
			})
		}
		result.schema = "soapTx5xx_v2"
		parsed = result
	case envelope.Body.InsertTx5xxCO2Sample != nil:
		result, err := parseBaseSample(envelope.Body.InsertTx5xxCO2Sample.baseSample)
		if err != nil {
			return device.ParseResult{}, err
		}
		if co2, ok, err := parseOptionalFloat(envelope.Body.InsertTx5xxCO2Sample.CO2, -9998, -9999); err != nil {
			return device.ParseResult{}, fmt.Errorf("read co2: %w", err)
		} else if ok {
			result.channels = append(result.channels, device.ChannelMeasurement{
				Name:  "CO2",
				Unit:  "ppm",
				Value: co2,
			})
		}
		result.schema = "soapTx5xxCO2"
		parsed = result
	default:
		return device.ParseResult{}, fmt.Errorf("unsupported SOAP body element")
	}

	return device.ParseResult{
		Serial:              parsed.serial,
		OccurredAt:          req.ReceivedAt.UTC(),
		ChannelMeasurements: parsed.channels,
		Response:            device.DefaultSuccessResponse(),
		Metadata: map[string]any{
			"secret":       req.Secret,
			"content_type": req.ContentType,
			"schema":       parsed.schema,
			"device_code":  parsed.deviceCode,
			"timer":        parsed.timer,
		},
	}, nil
}

type envelope struct {
	XMLName xml.Name `xml:"Envelope"`
	Body    body     `xml:"Body"`
}

type body struct {
	InsertTx5xxSample    *tx5xxSample    `xml:"InsertTx5xxSample"`
	InsertTx5xxCO2Sample *tx5xxCO2Sample `xml:"InsertTx5xxCO2Sample"`
}

type baseSample struct {
	PassKey   string `xml:"passKey"`
	Device    string `xml:"device"`
	Temp      string `xml:"temp"`
	RelHum    string `xml:"relHum"`
	CompQuant string `xml:"compQuant"`
	Alarms    string `xml:"alarms"`
	CompType  string `xml:"compType"`
	TempU     string `xml:"tempU"`
	Timer     string `xml:"timer"`
}

type tx5xxSample struct {
	baseSample
	Pressure  string `xml:"pressure"`
	PressureU string `xml:"pressureU"`
}

type tx5xxCO2Sample struct {
	baseSample
	CO2  string `xml:"co2"`
	Lev1 string `xml:"lev1"`
	Lev2 string `xml:"lev2"`
	Lev3 string `xml:"lev3"`
}

type parseResult struct {
	serial     string
	deviceCode string
	timer      string
	schema     string
	channels   []device.ChannelMeasurement
}

func parseBaseSample(sample baseSample) (parseResult, error) {
	serial := strings.TrimSpace(sample.PassKey)
	if serial == "" {
		return parseResult{}, fmt.Errorf("missing passKey")
	}

	result := parseResult{
		serial:     serial,
		deviceCode: strings.TrimSpace(sample.Device),
		timer:      strings.TrimSpace(sample.Timer),
		channels:   make([]device.ChannelMeasurement, 0, 4),
	}

	if temp, ok, err := parseOptionalFloat(sample.Temp, 9999, -9999); err != nil {
		return parseResult{}, fmt.Errorf("read temperature: %w", err)
	} else if ok {
		result.channels = append(result.channels, device.ChannelMeasurement{
			Name:  "Temperature",
			Unit:  normalizeTemperatureUnit(sample.TempU),
			Value: temp,
		})
	}

	if humidity, ok, err := parseOptionalFloat(sample.RelHum, 9999, -9999); err != nil {
		return parseResult{}, fmt.Errorf("read relative humidity: %w", err)
	} else if ok {
		result.channels = append(result.channels, device.ChannelMeasurement{
			Name:  "Relative humidity",
			Unit:  "%RH",
			Value: humidity,
		})
	}

	if compQuant, ok, err := parseOptionalFloat(sample.CompQuant, 9999, -9999); err != nil {
		return parseResult{}, fmt.Errorf("read computed quantity: %w", err)
	} else if ok {
		name := strings.TrimSpace(sample.CompType)
		if name == "" || strings.EqualFold(name, "n/a") {
			name = "Computed quantity"
		}
		result.channels = append(result.channels, device.ChannelMeasurement{
			Name:  name,
			Unit:  normalizeComputedUnit(name, sample.TempU),
			Value: compQuant,
		})
	}

	return result, nil
}

func parseOptionalFloat(raw string, errorValues ...float64) (float32, bool, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return 0, false, nil
	}

	parsed, err := strconv.ParseFloat(value, 32)
	if err != nil {
		return 0, false, err
	}

	for _, errorValue := range errorValues {
		if parsed == errorValue {
			return 0, false, nil
		}
	}

	return float32(parsed), true, nil
}

func normalizeTemperatureUnit(raw string) string {
	switch strings.ToUpper(strings.TrimSpace(raw)) {
	case "C", "°C":
		return "°C"
	case "F", "°F":
		return "°F"
	default:
		return strings.TrimSpace(raw)
	}
}

func normalizePressureUnit(raw string) string {
	return strings.TrimSpace(raw)
}

func normalizeComputedUnit(name, tempUnit string) string {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "dew point":
		return normalizeTemperatureUnit(tempUnit)
	default:
		return ""
	}
}
