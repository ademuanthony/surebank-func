package notify

import (
	"bytes"
	"context"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	text "text/template"

	"github.com/pkg/errors"
)

type BulkSmsNigeria struct {
	token       string
	sender      string
	templateDir string
	client      http.Client
}

var bulkSmsNigeria *BulkSmsNigeria

func init() {
	var err error
	bulkSmsNigeria, err = NewBulkSmsNigeria(os.Getenv("SMS_Auth_TOKEN"),
		"SUREBLTD", "./resources/templates/sms", *http.DefaultClient)
	if err != nil {
		log.Fatal(err)
	}
}

func NewBulkSmsNigeria(token, sender, sharedTemplateDir string, client http.Client) (*BulkSmsNigeria, error) {

	if token == "" {
		return nil, errors.New("SMS Auth token is required.")
	}

	if sender == "" {
		return nil, errors.New("SMS sender is required.")
	}

	templateDir := filepath.Join(sharedTemplateDir, "sms")
	if _, err := os.Stat(templateDir); os.IsNotExist(err) {
		return nil, errors.WithMessage(err, "SMS template directory does not exist.")
	}

	return &BulkSmsNigeria{
		token:       token,
		sender:      sender,
		templateDir: sharedTemplateDir,
		client:      client,
	}, nil
}

func (b *BulkSmsNigeria) Send(ctx context.Context, phoneNumber, templateName string, data map[string]interface{}) error {

	body, err := parseSMSTemplates(b.templateDir, templateName, data)
	if err != nil {
		return err
	}

	params := url.Values{}
	params.Add("api_token", b.token)
	params.Add("from", b.sender)
	params.Add("to", phoneNumber)
	params.Add("body", body)

	resp, err := b.client.Get("https://www.bulksmsnigeria.com/api/v1/sms/create?" + params.Encode())

	if err != nil {
		return err
	}

	respBody, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return errors.WithMessage(err, "cannot read response body")
	}

	if !strings.Contains(string(respBody), "") {
		return fmt.Errorf("cannot sent message, %s", string(respBody))
	}

	return nil
}

func Send(ctx context.Context, phoneNumber, templateName string, data map[string]interface{}) error {
	return bulkSmsNigeria.Send(ctx, phoneNumber, templateName, data)
}

func (b *BulkSmsNigeria) SendStr(ctx context.Context, phoneNumber, message string) error {
	params := url.Values{}
	params.Add("api_token", b.token)
	params.Add("from", b.sender)
	params.Add("to", phoneNumber)
	params.Add("body", message)

	resp, err := b.client.Get("https://www.bulksmsnigeria.com/api/v1/sms/create?" + params.Encode())

	if err != nil {
		return err
	}

	respBody, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return errors.WithMessage(err, "cannot read response body")
	}

	if !strings.Contains(string(respBody), "") {
		return fmt.Errorf("cannot sent message, %s", string(respBody))
	}

	return nil
}

func SendStr(ctx context.Context, phoneNumber, message string) error {
	return bulkSmsNigeria.SendStr(ctx, phoneNumber, message)
}

func parseSMSTemplates(templateDir, templateName string, data map[string]interface{}) (string, error) {
	txtFile := filepath.Join(templateDir, templateName+".txt")
	txtTmpl, err := text.ParseFiles(txtFile)
	if err != nil {
		return "", errors.WithMessage(err, "Failed to load SMS template.")
	}

	var txtDat bytes.Buffer
	if err := txtTmpl.Execute(&txtDat, data); err != nil {
		return "", errors.WithMessage(err, "Failed to parse SMS template.")
	}

	return string(txtDat.Bytes()), nil
}

type DepositSMSPayload struct {
	Name    string
	Amount  float64
	Balance float64
}
