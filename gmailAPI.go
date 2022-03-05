package googlemail4go

import (
	"context"
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

func BuildNewGmailAPI(client *http.Client, subject string, ctx context.Context) *GmailAPI {
	gmailService, err := gmail.NewService(ctx, option.WithHTTPClient(client))
	if err != nil {
		log.Println(err.Error())
		panic(err)
	}
	newGmailAPI := &GmailAPI{}
	newGmailAPI.GmailService = gmailService
	newGmailAPI.Subject = subject
	log.Printf("GmailAPI --> \nGmailService: %v, UserEmail: %s\n", &newGmailAPI.GmailService, subject)
	return newGmailAPI
}

type GmailAPI struct {
	GmailService *gmail.Service
	Subject      string
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

func (receiver *GmailAPI) QueryMessages(query string) []*gmail.Message {
	log.Println("User [" + receiver.Subject + "]: Gmail.Messages.List.Query{\"" + query + "\"}")
	messagesListCall := receiver.
		GmailService.
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

func (receiver GmailAPI) GetMessageByRFC822MsgID(rfc822MsgID string) *gmail.Message {
	return receiver.QueryMessages("rfc822msgid:" + rfc822MsgID)[0]
}

func (receiver *GmailAPI) ExportEmail(rfc822MsgID string) ([]byte, []EmailAttachment) {
	html := ""
	var emailBodyData []byte
	var attachments []EmailAttachment
	message := receiver.GetMessageByRFC822MsgID(rfc822MsgID)

	//Get the threads using that message ID
	threads, err := receiver.GmailService.Users.Threads.Get(receiver.Subject, message.ThreadId).Fields("*").Do()
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
	//	ts, _ := receiver.GmailService.Users.Threads.Get(receiver.Subject, message.ThreadId).Do()
	//	log.Println(ts)
	//	rawMessage, err := receiver.GmailService.Users.Messages.Get(receiver.Subject, message.ThreadId).Fields("*").Format("raw").Do()
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

func (email *GmailMessage) Send(googleGmail *GmailAPI) (*gmail.Message, GmailMessagePayload) {
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

func (receiver *GmailAPI) SendMessage(email *GmailMessage) (*gmail.Message, GmailMessagePayload) {
	if email.To == nil {
		log.Printf("No Recipients for email [%s]!!\n", email)
	} else if email.Body == "" {
		log.Printf("No Body in email [%s]!!\n", email)
	} else if email.Subject == "" {
		log.Printf("No subject for email [%s}!!\n", email)
	}
	return receiver.SendRawEmail(email.To, email.Cc, email.Bcc, email.FromName, email.Subject, email.Body, email.Attachments)
}

func (receiver *GmailAPI) SendRawEmail(to, cc, bcc []string, sender, subject, bodyHtml string, attachments []EmailAttachment) (*gmail.Message, GmailMessagePayload) {
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

	response, err := receiver.GmailService.
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

func (receiver *GmailAPI) GetDelegates() []*gmail.Delegate {
	response, err := receiver.GmailService.Users.Settings.Delegates.List(receiver.Subject).Do()
	if err != nil {
		log.Println(err.Error())
		return nil
	}
	return response.Delegates
}

func (receiver *GmailAPI) AddDelegate(newDelegates []string) error {
	if len(newDelegates) == 1 {
		_, err := receiver.GmailService.Users.Settings.Delegates.Create(receiver.Subject, &gmail.Delegate{DelegateEmail: newDelegates[0]}).Do()
		return err
	}

	maxExecutes := 10

	for {
		if len(newDelegates) < maxExecutes {
			maxExecutes = len(newDelegates)
		}

		wg := &sync.WaitGroup{}
		wg.Add(maxExecutes)

		for _, newDelegateEmail := range newDelegates[:maxExecutes] {
			go func() {
				defer wg.Done()
				newDelegate := &gmail.Delegate{DelegateEmail: newDelegateEmail}
				_, err := receiver.GmailService.Users.Settings.Delegates.Create(receiver.Subject, newDelegate).Do()
				if err != nil {
					log.Println(err.Error())
					panic(err)
				}
			}()
		}
		wg.Wait()

		newDelegates := newDelegates[maxExecutes:]
		if len(newDelegates) == 0 {
			break
		}
	}

	return nil

}

func (receiver *GmailAPI) RemoveDelegates(delegateEmails []string) error {
	if len(delegateEmails) == 1 {
		return receiver.GmailService.Users.Settings.Delegates.Delete(receiver.Subject, delegateEmails[0]).Do()
	}

	maxExecutes := 10
	batchCounter := 1
	for {
		batchCounter++
		if len(delegateEmails) < maxExecutes {
			maxExecutes = len(delegateEmails)
		}

		wg := &sync.WaitGroup{}
		wg.Add(maxExecutes)

		for _, delegateEmail := range delegateEmails[:maxExecutes] {
			go func() {
				defer wg.Done()
				err := receiver.GmailService.Users.Settings.Delegates.Delete(receiver.Subject, delegateEmail).Do()
				if err != nil {
					log.Println(err.Error())
					panic(err)
				}
				log.Printf("Account: %s delegate [%s] removed\n", receiver.Subject, delegateEmail)
			}()
		}
		wg.Wait()

		delegateEmails := delegateEmails[maxExecutes:]
		if len(delegateEmails) == 0 {
			break
		}
	}
	return nil
}

func (receiver *GmailAPI) GetMessageHeaders(query string) []GmailMessagePayload {
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

func (receiver *GmailAPI) GetPayload(messageID string) GmailMessagePayload {
	log.Println("Pulling [" + receiver.Subject + "] Gmail MsgId: " + messageID)
	response, err := receiver.GmailService.Users.Messages.Get(receiver.Subject, messageID).Fields("*").Do()
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

func (receiver *GmailAPI) GetMessageAttachmentsByRFC822MGSID(rfc822MsgID string) []EmailAttachment {
	var attachments []EmailAttachment
	for _, message := range receiver.QueryMessages("rfc822msgid:" + rfc822MsgID) {
		attachments = append(attachments, receiver.GetMessageAttachments(message.ThreadId)...)
	}
	return attachments
}

func (receiver *GmailAPI) GetMessageAttachments(threadId string) []EmailAttachment {
	//Get message by its thread id
	var attachments []EmailAttachment
	response, err := receiver.GmailService.Users.Threads.Get(receiver.Subject, threadId).Fields("*").Do()
	if err != nil {
		log.Println(err.Error())
		return nil
	}
	for _, message := range response.Messages {
		for _, messagePart := range message.Payload.Parts {
			if messagePart.Filename != "" {
				attachmentResponse, err := receiver.GmailService.
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

func (receiver *GmailAPI) GetLabel(labelName string) *gmail.Label {
	for _, label := range receiver.GetAllLabels() {
		if label.Name == labelName {
			return label
		}
	}
	return nil
}

func (receiver *GmailAPI) GetAllLabels() []*gmail.Label {
	listLabelsResponse, labelsListErr := receiver.GmailService.Users.Labels.List(receiver.Subject).Do()
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
