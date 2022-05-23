package googlemail4go

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/gmail/v1"
	"google.golang.org/api/option"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
)

type GmailAPI struct {
	GmailService *gmail.Service
	UserEmail    string
}

func BuildGmail3kWithOAuth2(subject string, scopes []string, clientSecret, authorizationToken []byte, ctx context.Context) *GmailAPI {
	config, err := google.ConfigFromJSON(clientSecret, scopes...)
	if err != nil {
		log.Println(err.Error())
		panic(err)
	}
	token := &oauth2.Token{}
	err = json.Unmarshal(authorizationToken, token)
	if err != nil {
		log.Println(err.Error())
		panic(err)
	}
	client := config.Client(context.Background(), token)
	return BuildGmail3k(client, subject, ctx)
}

func BuildGmail3kWithImpersonator(subject string, scopes []string, serviceAccountKey []byte, ctx context.Context) *GmailAPI {
	jwt, err := google.JWTConfigFromJSON(serviceAccountKey, scopes...)
	if err != nil {
		log.Println(err.Error())
		panic(err)
	}
	jwt.Subject = subject
	return BuildGmail3k(jwt.Client(ctx), subject, ctx)
}

func BuildGmail3k(client *http.Client, subject string, ctx context.Context) *GmailAPI {
	gmailService, err := gmail.NewService(ctx, option.WithHTTPClient(client))
	if err != nil {
		log.Println(err.Error())
		panic(err)
	}
	newGmailAPI := &GmailAPI{}
	newGmailAPI.GmailService = gmailService
	newGmailAPI.UserEmail = subject
	log.Printf("GmailAPI --> \nGmailService: %v, UserEmail: %s\n", &newGmailAPI.GmailService, subject)
	return newGmailAPI
}

type Draft struct {
	From        string
	SendAs      string
	Body        string
	Subject     string
	To          []string
	Cc          []string
	Bcc         []string
	Attachments []*Attachment
}

func (receiver *Draft) Send(gmail3k *GmailAPI) (*gmail.Message, error) {
	return gmail3k.SendDraft(receiver)
}

func DraftEmail(to, cc, bcc []string, subject, body string) *Draft {
	return &Draft{
		To:      to,
		Cc:      cc,
		Bcc:     bcc,
		Subject: subject,
		Body:    body,
	}
}

type ExportedMessage struct {
	Message     *gmail.Message
	Data        []byte
	ThreadId    string
	Headers     map[string]string
	Date        string
	From        string
	Body        *MessageBody
	Subject     string
	ReplyTo     string
	To          []string
	Cc          []string
	Bcc         []string
	Attachments []*Attachment
}

func (receiver *GmailAPI) ExportMessage(rfc822MsgID string) (*ExportedMessage, error) {
	log.Println("User [" + receiver.UserEmail + "]: Gmail.Messages.List.Query{\"" + rfc822MsgID + "\"}")
	messageList, err := receiver.Search("rfc822msgid:" + rfc822MsgID)
	originalMessage, err := receiver.GmailService.Users.Messages.Get(receiver.UserEmail, messageList[0].Id).Do()
	if err != nil {
		log.Println(err.Error())
		panic(err)
	}

	headers := make(map[string]string)
	for _, header := range originalMessage.Payload.Headers {
		headers[header.Name] = header.Value
	}

	rawMessageResponse, err := receiver.GmailService.Users.Messages.Get(receiver.UserEmail, originalMessage.Id).Format("raw").Do()
	if err != nil {
		log.Println(err.Error())
		panic(err)
	}

	decodedData, err := base64.URLEncoding.DecodeString(rawMessageResponse.Raw)
	if err != nil {
		log.Println(err.Error())
		return nil, err
	}

	exportedMessage := &ExportedMessage{}
	exportedMessage.ThreadId = originalMessage.ThreadId
	exportedMessage.Message = originalMessage
	exportedMessage.Data = decodedData
	exportedMessage.Headers = headers
	exportedMessage.Date = exportedMessage.Headers["Date"]
	exportedMessage.To = strings.Split(exportedMessage.Headers["To"], ",")
	exportedMessage.Cc = strings.Split(exportedMessage.Headers["Cc"], ",")
	exportedMessage.Bcc = strings.Split(exportedMessage.Headers["Bcc"], ",")
	exportedMessage.From = exportedMessage.Headers["From"]
	exportedMessage.ReplyTo = exportedMessage.Headers["Reply-To"]
	exportedMessage.Subject = exportedMessage.Headers["Subject"]
	exportedMessage.Body, err = GetBodyFromParts(originalMessage.Payload.Parts)
	if err != nil {
		log.Println(err.Error())
		panic(err)
	}
	attachments, err := receiver.GetThreadAttachments(originalMessage.ThreadId)
	if err != nil {
		log.Println(err.Error())
		return nil, err
	}
	exportedMessage.Attachments = attachments
	return exportedMessage, nil
}

func (receiver *GmailAPI) Search(query string) ([]*gmail.Message, error) {
	var messages []*gmail.Message
	nextPageToken := ""
	for {
		listMessagesResponse, err := receiver.GmailService.Users.Messages.
			List(receiver.UserEmail).
			PageToken(nextPageToken).
			Q(query).
			Fields("*").
			IncludeSpamTrash(true).Do()
		if err != nil {
			log.Println(err.Error())
			return nil, err
		}

		messages = append(messages, listMessagesResponse.Messages...)
		nextPageToken = listMessagesResponse.NextPageToken
		if nextPageToken == "" {
			break
		}
	}
	log.Println("UserEmail [" + receiver.UserEmail + "]: Gmail.Messages.List.Query{\"" + query + "\"} - Emails returned: " + fmt.Sprint(len(messages)))
	return messages, nil
}

func (receiver *GmailAPI) GetMessage(rfc822MsgId string) (*gmail.Message, error) {
	messages, err := receiver.Search("rfc822msgid:" + rfc822MsgId)
	if err != nil {
		log.Println(err.Error())
		return nil, err
	}
	return messages[0], nil
}

type MessageBody struct {
	Plain string
	Html  string
}

func GetBodyFromParts(rootParts []*gmail.MessagePart) (*MessageBody, error) {
	messageBody := &MessageBody{}
	for _, part := range rootParts {
		if part.MimeType == "multipart/alternative" {
			for _, messagePart := range part.Parts {
				body, err := base64.URLEncoding.DecodeString(messagePart.Body.Data)
				if err != nil {
					log.Println(err.Error())
					return nil, err
				}
				switch messagePart.MimeType {
				case "text/html":
					messageBody.Html = string(body)
				case "text/plain":
					messageBody.Plain = string(body)
				}
			}
		}
	}
	return messageBody, nil
}

func (receiver *GmailAPI) SendEmail(to, cc, bcc []string, sendAs, subject, body string, attachments []*Attachment) (*gmail.Message, error) {
	boundary := _string(32, "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ+/")
	messageBody := "Content-Type: multipart/mixed; boundary=" + boundary + " \n" +
		"MIME-Version: 1.0\n" +
		"To: " + strings.Join(to, ",") + "\n" +
		"CC: " + strings.Join(cc, ",") + "\n" +
		"BCC: " + strings.Join(bcc, ",") + "\n" +
		"From: " + sendAs + "<" + receiver.UserEmail + ">\n" +
		"UserEmail: " + subject + "\n\n" +
		"--" + boundary + "\n" +
		"Content-Type: text/html; charset=" + string('"') + "UTF-8" + string('"') + "\n" +
		"MIME-Version: 1.0\n" +
		"Content-Transfer-Encoding: 7bit\n\n" +
		body + "\n\n" +
		"--" + boundary + "\n"

	for i, attachment := range attachments {

		log.Printf("Attaching file [%d] of [%d]: %s", i, len(attachments)-1, attachment.Name)
		messageBody += "Content-Type: " + http.DetectContentType(attachment.Data) + "; Send=" + string('"') + attachment.Name + string('"') + " \n" +
			"MIME-Version: 1.0\n" +
			"Content-Transfer-Encoding: base64\n" +
			"Content-Disposition: attachment; filename=" + string('"') + attachment.Name + string('"') + " \n" +
			_chunkSplit(base64.RawStdEncoding.EncodeToString(attachment.Data), 76, "\n") + "--" + boundary + "\n"
	}

	newMessage := &gmail.Message{}
	rawMessage := []byte(messageBody)
	newMessage.Raw = base64.StdEncoding.EncodeToString(rawMessage)
	newMessage.Raw = strings.Replace(newMessage.Raw, "/", "_", -1)
	newMessage.Raw = strings.Replace(newMessage.Raw, "+", "-", -1)
	newMessage.Raw = strings.Replace(newMessage.Raw, "=", "", -1)
	totalAttachments := 0
	if attachments != nil {
		totalAttachments += len(attachments)
	}

	log.Printf("Sending Draft ->\nTo: %s\nFrom:%s<%s>\nUserEmail:%s\nTotal Attachments_: %d", to, sendAs, receiver.UserEmail, subject, totalAttachments)

	response, err := receiver.GmailService.
		Users.
		Messages.
		Send(receiver.UserEmail, newMessage).
		Fields("*").
		Do()

	if err != nil {
		log.Println(err.Error())
		return nil, err
	}

	log.Printf("Draft sent ->\nTo: %s\nFrom:%s<%s>\nUserEmail:%s\nTotal Attachments_: %d", to, sendAs, receiver.UserEmail, subject, totalAttachments)

	return response, nil
}

func (receiver *GmailAPI) SendDraft(draft *Draft) (*gmail.Message, error) {
	return receiver.SendEmail(draft.To, draft.Cc, draft.Bcc, draft.SendAs, draft.Subject, draft.Body, draft.Attachments)
}

//Delegates
func (receiver *GmailAPI) GetDelegates() (map[string]string, error) {
	response, err := receiver.GmailService.Users.Settings.Delegates.List(receiver.UserEmail).Do()
	if err != nil {
		log.Println(err.Error())
		return nil, err
	}
	delegateMap := make(map[string]string)
	for _, delegate := range response.Delegates {
		delegateMap[delegate.DelegateEmail] = delegate.VerificationStatus
	}
	return delegateMap, nil
}

func (receiver *GmailAPI) AddDelegates(userList []string) (map[string]string, error) {
	existingDelegates, err := receiver.GetDelegates()
	if err != nil {
		log.Println(err.Error())
		return nil, err
	}

	for i, userEmail := range userList {
		if existingDelegates[userEmail] != "" {
			userList = append(userList[:i], userList[i+1:]...)
			log.Printf("Mailbox [%s] already delegated to <%s>\n", receiver.UserEmail, userEmail)
		}
	}

	maxExecutes := 10
	for {
		if len(userList) == 0 {
			break
		} else if len(userList) < maxExecutes {
			maxExecutes = len(userList)
		}

		wg := &sync.WaitGroup{}
		wg.Add(maxExecutes)

		for _, newDelegateEmail := range userList[:maxExecutes] {
			go func() {
				defer wg.Done()
				if existingDelegates[newDelegateEmail] == "" {
					return
				}
				newDelegate := &gmail.Delegate{DelegateEmail: newDelegateEmail}
				response, err := receiver.GmailService.Users.Settings.Delegates.Create(receiver.UserEmail, newDelegate).Do()
				log.Printf("Mailbox [%s] has been delegated to <%s>\n", receiver.UserEmail, response.DelegateEmail)
				if err != nil {
					log.Println(err.Error())
				}
			}()
		}
		wg.Wait()

		userList = userList[maxExecutes:]
	}

	return receiver.GetDelegates()

}

func (receiver *GmailAPI) RemoveDelegates(userList []string) (map[string]string, error) {
	existingDelegates, err := receiver.GetDelegates()
	if err != nil {
		log.Println(err.Error())
		return nil, err
	}

	for i, userEmail := range userList {
		if existingDelegates[userEmail] == "" {
			log.Printf("Mailbox [%s] is not delegated to <%s>\n", receiver.UserEmail, userEmail)
			userList = append(userList[:i], userList[i+1:]...)
		}
	}

	maxExecutes := 10
	for {
		if len(userList) < maxExecutes {
			maxExecutes = len(userList)
		}

		wg := &sync.WaitGroup{}
		wg.Add(maxExecutes)

		for _, delegateEmail := range userList[:maxExecutes] {
			go func() {
				defer wg.Done()
				err := receiver.GmailService.Users.Settings.Delegates.Delete(receiver.UserEmail, delegateEmail).Do()
				if err != nil {
					log.Println(err.Error())
					panic(err)
				}
				log.Printf("Mailbox [%s] has removed delegate <%s>\n", receiver.UserEmail, delegateEmail)
				if err != nil {
					log.Println(err.Error())
					panic(err)
				}
			}()
		}
		wg.Wait()

		userList = userList[maxExecutes:]
		if len(userList) == 0 {
			break
		}
	}
	return receiver.GetDelegates()
}

//Labels
func (receiver *GmailAPI) GetAllLabels() ([]*gmail.Label, error) {
	listLabelsResponse, labelsListErr := receiver.GmailService.Users.Labels.List(receiver.UserEmail).Do()
	if labelsListErr != nil {
		log.Println(labelsListErr.Error())
		return nil, labelsListErr
	}
	return listLabelsResponse.Labels, nil
}

func (receiver *GmailAPI) GetLabel(labelName string) (*gmail.Label, error) {
	allLabels, err := receiver.GetAllLabels()
	if err != nil {
		log.Println(err.Error())
		return nil, err
	}

	var targetLabel *gmail.Label
	for _, label := range allLabels {
		if label.Name == labelName {
			targetLabel = label
		}
	}

	return targetLabel, nil
}

//Attachments
type Attachment struct {
	Name string `json:"name"`
	Data []byte `json:"data"`
}

func AttachmentFromPath(filePath string) *Attachment {
	osFile, err := os.Open(filePath)
	if err != nil {
		log.Println(err.Error())
		return nil
	}

	data, err := ioutil.ReadAll(osFile)
	if err != nil {
		log.Println(err.Error())
		return nil
	}
	return &Attachment{
		Name: osFile.Name(),
		Data: data,
	}
}

func (receiver *GmailAPI) GetMessageAttachmentsByRFC822MGSID(rfc822MsgID string) ([]*Attachment, error) {
	message, err := receiver.ExportMessage(rfc822MsgID)
	if err != nil {
		log.Println(err.Error())
		return nil, err
	}
	return receiver.GetThreadAttachments(message.ThreadId)
}

func (receiver *GmailAPI) GetThreadAttachments(threadId string) ([]*Attachment, error) {
	//ExportMessage message by its thread id
	var attachments []*Attachment
	response, err := receiver.GmailService.Users.Threads.Get(receiver.UserEmail, threadId).Fields("*").Do()
	if err != nil {
		log.Println(err.Error())
		return nil, err
	}
	for _, message := range response.Messages {
		for _, messagePart := range message.Payload.Parts {
			if messagePart.Filename != "" {
				attachmentResponse, err := receiver.GmailService.
					Users.
					Messages.
					Attachments.
					Get(receiver.UserEmail, threadId, messagePart.Body.AttachmentId).
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
				emailAttachment := &Attachment{
					Name: fileName,
					Data: data,
				}
				attachments = append(attachments, emailAttachment)
			}
		}
	}
	return attachments, nil
}

//--------------------------------------------------------------------------------------------------------------------*/

func _chunkSplit(body string, limit int, end string) string {
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

// _string generates a random string using only letters provided in the letters parameter
// if user ommit letters parameters, this function will use defLetters instead
func _string(n int, letters ...string) string {
	var letterRunes []rune
	if len(letters) == 0 {
		letterRunes = []rune("0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")
	} else {
		letterRunes = []rune(letters[0])
	}

	var bb bytes.Buffer
	bb.Grow(n)
	l := uint32(len(letterRunes))
	// on each loop, generate one random rune and append to output
	for i := 0; i < n; i++ {
		b := make([]byte, 4)
		_, err := rand.Read(b)
		if err != nil {
			panic(err)
		}
		bb.WriteRune(letterRunes[binary.BigEndian.Uint32(b)%l])
	}
	return bb.String()
}
