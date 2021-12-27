package googlemail4go

import (
	"context"
	"encoding/base64"
	"github.com/thanhpk/randstr"

	"fmt"
	"google.golang.org/api/gmail/v1"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/option"
	"io/ioutil"
	"log"
	"mime"
	"path/filepath"
	"strings"
	"sync"
)

var ctx = context.Background()

func Initialize(option *option.ClientOption, subject string) *GoogleGmail {
	service, err := gmail.NewService(ctx, *option)
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
	GmailMessageID  string
	From            string
	To              string
	Cc              string
	Subject         string
	Rfc822MessageId string
	Date            string
	DeliveredTo     string
	Received        string
	Body            string
}

type GmailMessage struct {
	Body            string
	FromName        string
	Subject         string
	To              []string
	Cc              []string
	Bcc             []string
	AttachmentPaths []string
	SendX           func()
}

func (email *GmailMessage) Send(googleGmail *GoogleGmail) {
	googleGmail.SendEmail(email)
}

func (receiver *GoogleGmail) SendEmail(email *GmailMessage) {
	if email.To == nil {
		log.Printf("No Recipients for email [%s]!!\n", email)
	} else if email.Body == "" {
		log.Printf("No Body in email [%s]!!\n", email)
	} else if email.Subject == "" {
		log.Printf("No subject for email [%s}!!\n", email)
	}
	receiver.SendRawEmail(email.To, email.Cc, email.Bcc, email.FromName, email.Subject, email.Body, email.AttachmentPaths)
}

func (receiver *GoogleGmail) SendRawEmail(to, cc, bcc []string, sender, subject, bodyHtml string, filePaths []string) *gmail.Message {
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

	for i, attachmentPath := range filePaths {
		log.Printf("Attaching file [%d] of [%d]: %s", i, len(filePaths)-1, attachmentPath)
		fileData, err := ioutil.ReadFile(attachmentPath)
		if err != nil {
			log.Println(err.Error())
			panic(err)
		}
		fileName := filepath.Base(attachmentPath)
		ext := filepath.Ext(fileName)
		mimeExtension := mime.TypeByExtension(ext)

		messageBody += "Content-Type: " + mimeExtension + "; SendEmail=" + string('"') + fileName + string('"') + " \n" +
			"MIME-Version: 1.0\n" +
			"Content-Transfer-Encoding: base64\n" +
			"Content-Disposition: attachment; filename=" + string('"') + fileName + string('"') + " \n" +
			chunkSplit(base64.RawStdEncoding.EncodeToString(fileData), 76, "\n") + "--" + boundary + "\n"
	}

	rawMessage := []byte(messageBody)
	message.Raw = base64.StdEncoding.EncodeToString(rawMessage)
	message.Raw = strings.Replace(message.Raw, "/", "_", -1)
	message.Raw = strings.Replace(message.Raw, "+", "-", -1)
	message.Raw = strings.Replace(message.Raw, "=", "", -1)

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
	totalAttachments := 0
	if filePaths != nil {
		totalAttachments += len(filePaths)
	}
	log.Printf("GmailMessage sent:\nTo: %s\nFrom:%s<%s>\nSubject:%s\nAttachments: %d", to, sender, receiver.Subject, subject, totalAttachments)

	return response
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

func (receiver *GoogleGmail) QueryMessages(query string) []*gmail.Message {
	log.Println("User [" + receiver.Subject + "]: Gmail.Messages.List.Query{\"" + query + "\"}")
	messagesListCall := receiver.Service.Users.Messages.List(receiver.Subject).Q(query).MaxResults(500)
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
	log.Println("User [" + receiver.Subject + "]: Gmail.Messages.List.Query{\"" + query + "} - Emails returned: " + fmt.Sprint(len(messages)))
	return messages
}

func (receiver *GoogleGmail) GetMessageHeaders(query string) []GmailMessagePayload {
	messages := receiver.QueryMessages(query)
	var gmailMessagePayloads []GmailMessagePayload
	counter := 0
	maxRoutines := 1
	totalItems := len(messages)
	worker := func(gmailMessageID string, waitGroup *sync.WaitGroup) {
		log.Println("Set [" + fmt.Sprint(counter) + "] of [" + fmt.Sprint(totalItems) + "]")
		gmailMessagePayloads = append(gmailMessagePayloads, receiver.GetGmailMessageByThreadID(gmailMessageID))
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

func (receiver *GoogleGmail) GetGmailMessageByThreadID(threadID string) GmailMessagePayload {
	log.Println("Pulling [" + receiver.Subject + "] Gmail MsgId: " + threadID)
	response, err := receiver.Service.Users.Messages.Get(receiver.Subject, threadID).Fields("id,threadId,payload").Do()
	if err != nil {
		ger := GetGoogleErrorResponse(err)
		log.Println(ger.Message, ger.Details)
		panic(err)
	}

	headersMap := make(map[string]string)
	for _, header := range response.Payload.Headers {
		/*if strings.ToLower(header.Name) == "date" {
			rawTime := header.Value
			parts := strings.Split(rawTime, "(")
			rawTime = strings.Trim(parts[0], " ")
			printTime, err := time.Parse(time.RFC3339, rawTime)
			if err != nil {
				log.Println(err.Error())
				panic(err)
			}
			headersMap[strings.ToLower(header.Name)] = fmt.Sprint(printTime)
			continue
		}*/
		headersMap[strings.ToLower(header.Name)] = header.Value
	}

	return GmailMessagePayload{
		GmailMessageID:  threadID,
		From:            headersMap["from"],
		To:              headersMap["to"],
		Cc:              headersMap["cc"],
		Subject:         headersMap["subject"],
		Rfc822MessageId: headersMap["message-id"],
		Date:            headersMap["date"],
		DeliveredTo:     headersMap["delivered-to"],
		Received:        headersMap["received"],
	}
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
