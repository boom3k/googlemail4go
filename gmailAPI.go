package googlemail4go

import (
	"context"
	_ "embed"
	"encoding/base64"
	"fmt"
	"github.com/thanhpk/randstr"
	"google.golang.org/api/gmail/v1"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/option"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
)

//go:embed client_secret.json
var client_secret []byte

//go:embed token.json
var token []byte

var ctx = context.Background()

//func main() {
//	oauth2Token := &oauth2.Token{}
//	json.Unmarshal(token, oauth2Token)
//	config, _ := google.ConfigFromJSON(client_secret)
//	api := Build(config.Client(context.Background(), oauth2Token), "ramel@ocie.io")
//	rfc8222MsgID := ""
//	body, attachments := api.ExportEmail(rfc8222MsgID)
//	os.Mkdir(rfc8222MsgID, os.ModePerm)
//	os.WriteFile(rfc8222MsgID+string(os.PathSeparator)+rfc8222MsgID+".eml", body, os.ModePerm)
//	if attachments != nil {
//		for _, attachment := range attachments {
//			os.WriteFile(rfc8222MsgID+string(os.PathSeparator)+attachment.Filename, attachment.Data, os.ModePerm)
//		}
//	}
//
//	//a1, _ := ioutil.ReadFile(".gitignore")
//	//message := NewMessage([]string{"boomboom3k@gmail.com"}, "Test - "+time.Now().String(), "BODY").AddAttachment(".gitignore", a1)
//	//response, payload := api.SendMessage(message)
//	//x := strings.Join(response.ServerResponse.Header["Date"], " ")
//	//log.Println(message, payload, response, x)
//}

func Build(client *http.Client, subject string) *GoogleGmail {
	service, err := gmail.NewService(ctx, option.WithHTTPClient(client))
	if err != nil {
		log.Println(err.Error())
		panic(err)
	}

	delegatesService := gmail.NewUsersSettingsDelegatesService(service)
	if delegatesService == nil {
		log.Println("No Deleagets Service!")
	}
	log.Printf("Initialized GoogleGmail4Go as (%s)\n", subject)
	return &GoogleGmail{Service: service, Subject: subject}
}

type GoogleGmail struct {
	Service          *gmail.Service
	DelegatesService *gmail.UsersSettingsDelegatesService
	Subject          string
}

type GmailMessagePayload struct {
	ThreadID        string
	From            string
	To              []string
	Cc              []string
	Bcc             []string
	Subject         string
	Rfc822MessageId string
	Date            string
	DeliveredTo     string
	Received        string
	Body            string
	Attachments     []EmailAttachment
}

type EmailAttachment struct {
	Filename   string
	Extenstion string
	Data       []byte
	MimeType   string
}

type GmailMessage struct {
	Body        string
	FromName    string
	Subject     string
	To          []string
	Cc          []string
	Bcc         []string
	Attachments []EmailAttachment
}

func NewMessage(to []string, subject, body string) *GmailMessage {
	return &GmailMessage{
		To:      to,
		Subject: subject,
		Body:    body,
	}
}

func (receiver *GoogleGmail) QueryMessages(query string) []*gmail.Message {
	log.Println("User [" + receiver.Subject + "]: Gmail.Messages.List.Query{\"" + query + "\"}")
	messagesListCall := receiver.
		Service.
		Users.
		Messages.
		List(receiver.Subject).
		Q(query).
		Fields("*").
		IncludeSpamTrash(true).
		MaxResults(500)
	response := &gmail.ListMessagesResponse{}
	pullMessages := func() []*gmail.Message {
		response, _ = messagesListCall.Do()
		var messagesReturned []*gmail.Message
		for _, m := range response.Messages {
			messagesReturned = append(messagesReturned, m)
		}
		return messagesReturned
	}
	messages := pullMessages()
	for response.NextPageToken != "" {
		log.Println("Messages Thus Far: ", len(messages), "Current PageToken: ["+response.NextPageToken+"]")
		messagesListCall.PageToken(response.NextPageToken)
		messages = append(messages, pullMessages()...)
	}
	log.Println("User [" + receiver.Subject + "]: Gmail.Messages.List.Query{\"" + query + "\"} - Emails returned: " + fmt.Sprint(len(messages)))
	return messages
}

func (receiver GoogleGmail) GetMessageByRFC822MsgID(rfc822MsgID string) *gmail.Message {
	return receiver.QueryMessages("rfc822msgid:" + rfc822MsgID)[0]
}

func (receiver *GoogleGmail) ExportEmail(rfc822MsgID string) ([]byte, []EmailAttachment) {
	html := ""
	var emailBodyData []byte
	var attachments []EmailAttachment
	message := receiver.GetMessageByRFC822MsgID(rfc822MsgID)

	//Get the threads using that message ID
	threads, err := receiver.Service.Users.Threads.Get(receiver.Subject, message.ThreadId).Fields("*").Do()
	if err != nil {
		log.Println(err.Error())
		panic(err)
	}

	for _, message := range threads.Messages {
		payload := receiver.GetPayload(message.Id)
		html += "From: " + payload.From + "<br />"
		html += "To: " + strings.Join(payload.To, ",") + "<br />"
		html += "Date: " + payload.Date + "<br />"
		html += "Subject: " + payload.Subject + "<br />"
		html += "<hr />"
		html += strings.ReplaceAll(payload.Body, "/ < img[^ >]* > /", "")
		html += "<hr />"
	}

	//for _, message := range messages {
	//	payload := receiver.GetPayload(message.ThreadId)
	//	log.Println(payload)
	//	ts, _ := receiver.Service.Users.Threads.Get(receiver.Subject, message.ThreadId).Do()
	//	log.Println(ts)
	//	rawMessage, err := receiver.Service.Users.Messages.Get(receiver.Subject, message.ThreadId).Fields("*").Format("raw").Do()
	//	if err != nil {
	//		log.Println(err.Error())
	//		panic(err)
	//	}
	//	log.Println(html)
	//	emailBodyData, err = base64.URLEncoding.DecodeString(rawMessage.Raw)
	//	if err != nil {
	//		log.Println(err.Error())
	//		panic(err)
	//	}
	//	attachments = append(attachments, receiver.GetMessageAttachments(message.ThreadId)...)
	//}
	return emailBodyData, attachments
}

func (email *GmailMessage) Send(googleGmail *GoogleGmail) (*gmail.Message, GmailMessagePayload) {
	return googleGmail.SendMessage(email)
}

func (email *GmailMessage) AddAttachment(fileNameWithExtension string, data []byte) *GmailMessage {
	email.Attachments = append(email.Attachments, EmailAttachment{
		Filename:   fileNameWithExtension,
		Data:       data,
		MimeType:   http.DetectContentType(data),
		Extenstion: filepath.Ext(fileNameWithExtension),
	})
	return email
}

func (receiver *GoogleGmail) SendMessage(email *GmailMessage) (*gmail.Message, GmailMessagePayload) {
	if email.To == nil {
		log.Printf("No Recipients for email [%s]!!\n", email)
	} else if email.Body == "" {
		log.Printf("No Body in email [%s]!!\n", email)
	} else if email.Subject == "" {
		log.Printf("No subject for email [%s}!!\n", email)
	}
	return receiver.SendRawEmail(email.To, email.Cc, email.Bcc, email.FromName, email.Subject, email.Body, email.Attachments)
}

func (receiver *GoogleGmail) SendRawEmail(to, cc, bcc []string, sender, subject, bodyHtml string, attachments []EmailAttachment) (*gmail.Message, GmailMessagePayload) {
	message := &gmail.Message{}

	boundary := randstr.Base64(32)
	messageBody := "Content-Type: multipart/mixed; boundary=" + boundary + " \n" +
		"MIME-Version: 1.0\n" +
		"To: " + strings.Join(to, ",") + "\n" +
		"CC: " + strings.Join(cc, ",") + "\n" +
		"BCC: " + strings.Join(bcc, ",") + "\n" +
		"From: " + sender + "<" + receiver.Subject + ">\n" +
		"Subject: " + subject + "\n\n" +
		"--" + boundary + "\n" +
		"Content-Type: text/html; charset=" + string('"') + "UTF-8" + string('"') + "\n" +
		"MIME-Version: 1.0\n" +
		"Content-Transfer-Encoding: 7bit\n\n" +
		bodyHtml + "\n\n" +
		"--" + boundary + "\n"

	for i, attachment := range attachments {

		log.Printf("Attaching file [%d] of [%d]: %s", i, len(attachments)-1, attachment.Filename)
		messageBody += "Content-Type: " + attachment.MimeType + "; SendMessage=" + string('"') + attachment.Filename + string('"') + " \n" +
			"MIME-Version: 1.0\n" +
			"Content-Transfer-Encoding: base64\n" +
			"Content-Disposition: attachment; filename=" + string('"') + attachment.Filename + string('"') + " \n" +
			chunkSplit(base64.RawStdEncoding.EncodeToString(attachment.Data), 76, "\n") + "--" + boundary + "\n"
	}

	rawMessage := []byte(messageBody)
	message.Raw = base64.StdEncoding.EncodeToString(rawMessage)
	message.Raw = strings.Replace(message.Raw, "/", "_", -1)
	message.Raw = strings.Replace(message.Raw, "+", "-", -1)
	message.Raw = strings.Replace(message.Raw, "=", "", -1)
	totalAttachments := 0
	if attachments != nil {
		totalAttachments += len(attachments)
	}

	log.Printf("Sending Email ->\nTo: %s\nFrom:%s<%s>\nSubject:%s\nTotal Attachments: %d", to, sender, receiver.Subject, subject, totalAttachments)

	response, err := receiver.Service.
		Users.
		Messages.
		Send(receiver.Subject, message).
		Fields("*").
		Do()

	if err != nil {
		log.Println(err.Error())
		panic(err)
	}

	log.Printf("GmailMessage sent ->\nTo: %s\nFrom:%s<%s>\nSubject:%s\nTotal Attachments: %d", to, sender, receiver.Subject, subject, totalAttachments)

	return response, receiver.GetPayload(response.ThreadId)
}

func (receiver *GoogleGmail) GetDelegates() []*gmail.Delegate {
	response, err := receiver.DelegatesService.List(receiver.Subject).Do()
	if err != nil {
		log.Println(err.Error())
		return nil
	}
	return response.Delegates
}

func (receiver *GoogleGmail) AddDelegate(newDelegates string) *gmail.Delegate {
	response, err := receiver.Service.Users.Settings.Delegates.Create(receiver.Subject, &gmail.Delegate{DelegateEmail: newDelegates}).Do()
	if err != nil {
		log.Println(err.Error())
		panic(err)
	}
	return response
}

func (receiver *GoogleGmail) GetMessageHeaders(query string) []GmailMessagePayload {
	messages := receiver.QueryMessages(query)
	var gmailMessagePayloads []GmailMessagePayload
	counter := 0
	maxRoutines := 1
	totalItems := len(messages)
	worker := func(gmailMessageID string, waitGroup *sync.WaitGroup) {
		log.Println("Set [" + fmt.Sprint(counter) + "] of [" + fmt.Sprint(totalItems) + "]")
		gmailMessagePayloads = append(gmailMessagePayloads, receiver.GetPayload(gmailMessageID))
		waitGroup.Done()
		counter++
	}
	for len(messages) != 0 {
		if len(messages) < maxRoutines {
			currentMessages := messages[:]
			waitGroup := sync.WaitGroup{}
			waitGroup.Add(len(currentMessages))
			for i := range currentMessages {
				go worker(currentMessages[i].Id, &waitGroup)
			}
			waitGroup.Wait()
			break
		} else {
			currentMessages := messages[:maxRoutines]
			waitGroup := sync.WaitGroup{}
			waitGroup.Add(len(currentMessages))
			for i := range currentMessages {
				go worker(currentMessages[i].Id, &waitGroup)
			}
			waitGroup.Wait()
			messages = append(messages[:0], messages[maxRoutines:]...)
		}
	}
	return gmailMessagePayloads
}

func (receiver *GoogleGmail) GetPayload(messageID string) GmailMessagePayload {
	log.Println("Pulling [" + receiver.Subject + "] Gmail MsgId: " + messageID)
	response, err := receiver.Service.Users.Messages.Get(receiver.Subject, messageID).Fields("*").Do()
	if err != nil {
		ger := GetGoogleErrorResponse(err)
		log.Println(ger.Message, ger.Details)
		panic(err)
	}

	headersMap := make(map[string]string)
	for _, header := range response.Payload.Headers {
		log.Printf("%s: %s -> %v", messageID, header.Name, header.Value)
		headersMap[strings.ToLower(header.Name)] = header.Value
	}

	return GmailMessagePayload{
		ThreadID:        messageID,
		Body:            headersMap["body"],
		From:            headersMap["from"],
		To:              strings.Split(headersMap["to"], ","),
		Cc:              strings.Split(headersMap["cc"], ","),
		Bcc:             strings.Split(headersMap["bcc"], ","),
		Subject:         headersMap["subject"],
		Rfc822MessageId: headersMap["message-id"],
		Date:            headersMap["date"],
		DeliveredTo:     headersMap["delivered-to"],
		Received:        headersMap["received"],
		Attachments:     receiver.GetMessageAttachments(messageID),
	}
}

func (receiver *GoogleGmail) GetMessageAttachmentsByRFC822MGSID(rfc822MsgID string) []EmailAttachment {
	var attachments []EmailAttachment
	for _, message := range receiver.QueryMessages("rfc822msgid:" + rfc822MsgID) {
		attachments = append(attachments, receiver.GetMessageAttachments(message.ThreadId)...)
	}
	return attachments
}

func (receiver *GoogleGmail) GetMessageAttachments(threadId string) []EmailAttachment {
	//Get message by its thread id
	var attachments []EmailAttachment
	response, err := receiver.Service.Users.Threads.Get(receiver.Subject, threadId).Fields("*").Do()
	if err != nil {
		log.Println(err.Error())
		return nil
	}
	for _, message := range response.Messages {
		for _, messagePart := range message.Payload.Parts {
			if messagePart.Filename != "" {
				attachmentResponse, err := receiver.Service.
					Users.
					Messages.
					Attachments.
					Get(receiver.Subject, threadId, messagePart.Body.AttachmentId).
					Fields("*").
					Do()
				if err != nil {
					log.Println(err.Error())
					panic(err)
				}
				data, err := base64.URLEncoding.DecodeString(attachmentResponse.Data)
				if err != nil {
					log.Println(err.Error())
					panic(err)
				}
				fileName := messagePart.Filename
				attachment := &EmailAttachment{
					Filename:   fileName,
					Data:       data,
					Extenstion: filepath.Ext(fileName),
					MimeType:   http.DetectContentType(data),
				}
				attachments = append(attachments, *attachment)
			}
		}

	}
	return attachments
}

func (receiver *GoogleGmail) GetLabel(labelName string) *gmail.Label {
	for _, label := range receiver.GetAllLabels() {
		if label.Name == labelName {
			return label
		}
	}
	return nil
}

func (receiver *GoogleGmail) GetAllLabels() []*gmail.Label {
	listLabelsResponse, labelsListErr := receiver.Service.Users.Labels.List(receiver.Subject).Do()
	if labelsListErr != nil {
		log.Println(labelsListErr.Error())
		panic(labelsListErr)
	}

	return listLabelsResponse.Labels
}

func GetGoogleErrorResponse(err error) *googleapi.Error {
	return err.(*googleapi.Error)
}

func chunkSplit(body string, limit int, end string) string {
	var charSlice []rune

	// push characters to slice
	for _, char := range body {
		charSlice = append(charSlice, char)
	}

	var result = ""

	for len(charSlice) >= 1 {
		// convert slice/array back to string
		// but insert end at specified limit
		result = result + string(charSlice[:limit]) + end

		// discard the elements that were copied over to result
		charSlice = charSlice[limit:]

		// change the limit
		// to cater for the last few words in
		if len(charSlice) < limit {
			limit = len(charSlice)
		}
	}
	return result
}
