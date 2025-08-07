package pngmeta

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"gf-lt/models"
	"io"
	"log/slog"
	"os"
	"path"
	"strings"
)

const (
	embType     = "tEXt"
	cKey        = "chara"
	IEND        = "IEND"
	header      = "\x89PNG\r\n\x1a\n"
	writeHeader = "\x89\x50\x4E\x47\x0D\x0A\x1A\x0A"
)

type PngEmbed struct {
	Key   string
	Value string
}

func (c PngEmbed) GetDecodedValue() (*models.CharCardSpec, error) {
	data, err := base64.StdEncoding.DecodeString(c.Value)
	if err != nil {
		return nil, err
	}
	card := &models.CharCardSpec{}
	if err := json.Unmarshal(data, &card); err != nil {
		return nil, err
	}
	specWrap := &models.Spec2Wrapper{}
	if card.Name == "" {
		if err := json.Unmarshal(data, &specWrap); err != nil {
			return nil, err
		}
		return &specWrap.Data, nil
	}
	return card, nil
}

func extractChar(fname string) (*PngEmbed, error) {
	data, err := os.ReadFile(fname)
	if err != nil {
		return nil, err
	}
	reader := bytes.NewReader(data)
	pr, err := NewPNGStepReader(reader)
	if err != nil {
		return nil, err
	}
	for {
		step, err := pr.Next()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
		}
		if step.Type() != embType {
			if _, err := io.Copy(io.Discard, step); err != nil {
				return nil, err
			}
		} else {
			buf, err := io.ReadAll(step)
			if err != nil {
				return nil, err
			}
			dataInstep := string(buf)
			values := strings.Split(dataInstep, "\x00")
			if len(values) == 2 {
				return &PngEmbed{Key: values[0], Value: values[1]}, nil
			}
		}
		if err := step.Close(); err != nil {
			return nil, err
		}
	}
	return nil, errors.New("failed to find embedded char in png: " + fname)
}

func ReadCard(fname, uname string) (*models.CharCard, error) {
	pe, err := extractChar(fname)
	if err != nil {
		return nil, err
	}
	charSpec, err := pe.GetDecodedValue()
	if err != nil {
		return nil, err
	}
	if charSpec.Name == "" {
		return nil, fmt.Errorf("failed to find role; fname %s", fname)
	}
	return charSpec.Simplify(uname, fname), nil
}

func ReadCardJson(fname string) (*models.CharCard, error) {
	data, err := os.ReadFile(fname)
	if err != nil {
		return nil, err
	}
	card := models.CharCard{}
	if err := json.Unmarshal(data, &card); err != nil {
		return nil, err
	}
	return &card, nil
}

func ReadDirCards(dirname, uname string, log *slog.Logger) ([]*models.CharCard, error) {
	files, err := os.ReadDir(dirname)
	if err != nil {
		return nil, err
	}
	resp := []*models.CharCard{}
	for _, f := range files {
		if f.IsDir() {
			continue
		}
		if strings.HasSuffix(f.Name(), ".png") {
			fpath := path.Join(dirname, f.Name())
			cc, err := ReadCard(fpath, uname)
			if err != nil {
				log.Warn("failed to load card", "error", err, "card", fpath)
				continue
			}
			resp = append(resp, cc)
		}
		if strings.HasSuffix(f.Name(), ".json") {
			fpath := path.Join(dirname, f.Name())
			cc, err := ReadCardJson(fpath)
			if err != nil {
				log.Warn("failed to load card", "error", err, "card", fpath)
				continue
			}
			cc.FirstMsg = strings.ReplaceAll(strings.ReplaceAll(cc.FirstMsg, "{{char}}", cc.Role), "{{user}}", uname)
			cc.SysPrompt = strings.ReplaceAll(strings.ReplaceAll(cc.SysPrompt, "{{char}}", cc.Role), "{{user}}", uname)
			resp = append(resp, cc)
		}
	}
	return resp, nil
}
