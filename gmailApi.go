/*
   Copyright (C) 2021-present Mirko Perillo and contributors

   This file is part of gmail-downloader.

   gmail-downloader is free software: you can redistribute it and/or modify
   it under the terms of the GNU General Public License as published by
   the Free Software Foundation, either version 3 of the License, or
   (at your option) any later version.

   gmail-downloader is distributed in the hope that it will be useful,
   but WITHOUT ANY WARRANTY; without even the implied warranty of
   MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
   GNU General Public License for more details.

   You should have received a copy of the GNU General Public License
   along with ts-converter.  If not, see <http://www.gnu.org/licenses/>.
*/

package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"

	"golang.org/x/net/context"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/gmail/v1"

	"encoding/base64"
	"errors"
)

const ENV_HOME_VAR = "GDOWN_HOME"

type Attachment struct {
	Id       string
	Filename string
	Content  []byte
	Skip     bool
}

// Retrieve a token, saves the token, then returns the generated client.
func getClient(config *oauth2.Config) *http.Client {
	// The file token.json stores the user's access and refresh tokens, and is
	// created automatically when the authorization flow completes for the first
	// time.
	home := getHomeFolder()
	tokFile := fmt.Sprintf("%s/token.json", home)
	tok, err := tokenFromFile(tokFile)
	if err != nil {
		tok = getTokenFromWeb(config)
		saveToken(tokFile, tok)
	}
	return config.Client(context.Background(), tok)
}

func getHomeFolder() string {
	home, isSet := os.LookupEnv(ENV_HOME_VAR)
	if !isSet {
		home = "./"
	}
	return home
}

// Request a token from the web, then returns the retrieved token.
func getTokenFromWeb(config *oauth2.Config) *oauth2.Token {
	authURL := config.AuthCodeURL("state-token", oauth2.AccessTypeOffline)
	fmt.Printf("Go to the following link in your browser then type the "+
		"authorization code: \n%v\n", authURL)

	var authCode string
	if _, err := fmt.Scan(&authCode); err != nil {
		log.Fatalf("Unable to read authorization code: %v", err)
	}

	tok, err := config.Exchange(context.TODO(), authCode)
	if err != nil {
		log.Fatalf("Unable to retrieve token from web: %v", err)
	}
	return tok
}

// Retrieves a token from a local file.
func tokenFromFile(file string) (*oauth2.Token, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	tok := &oauth2.Token{}
	err = json.NewDecoder(f).Decode(tok)
	return tok, err
}

// Saves a token to a file path.
func saveToken(path string, token *oauth2.Token) {
	fmt.Printf("Saving credential file to: %s\n", path)
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		log.Fatalf("Unable to cache oauth token: %v", err)
	}
	defer f.Close()
	json.NewEncoder(f).Encode(token)
}

func messagesByLabel(srv *gmail.Service, labelId string) ([]*gmail.Message, error) {
	msgs, err := srv.Users.Messages.List("me").LabelIds(labelId).MaxResults(500).Do()
	if err != nil {
		return nil, err
	}
	var messages []*gmail.Message = make([]*gmail.Message, 0)

	for _, msg := range msgs.Messages {
		completeMsg, err := srv.Users.Messages.Get("me", msg.Id).Do()
		if err != nil {
			return messages, err
		}
		messages = append(messages, completeMsg)
	}
	return messages, err
}

func labelId(srv *gmail.Service, name string) (string, error) {
	var labelId string
	user := "me"
	r, err := srv.Users.Labels.List(user).Do()
	if err != nil {
		return labelId, err
	}
	for _, l := range r.Labels {
		if l.Name == name {
			labelId = l.Id
			return labelId, nil
		}
	}
	return labelId, errors.New(fmt.Sprintf("Label %s not found", name))
}

func decodeAttachment(encodedContent string) ([]byte, error) {
	return base64.URLEncoding.DecodeString(encodedContent)
}

func attachments(srv *gmail.Service, mail *gmail.Message, path string, notOverwrite bool) ([]Attachment, error) {
	user := "me"
	attachments := make([]Attachment, 0)
	if mail.Payload != nil {
		for _, part := range mail.Payload.Parts {
			if part.Body.AttachmentId != "" {
				log.Printf("attachment filename: %v", part.Filename)
				attachments = append(attachments, Attachment{Id: part.Body.AttachmentId, Filename: part.Filename})
			}
		}

		for pos, attachment := range attachments {
			filePath := fmt.Sprintf("%v/%v", path, attachment.Filename)
			_, err := os.Stat(filePath)
			if err == nil && notOverwrite {
				attachment.Skip = true
				attachments[pos] = attachment
				continue
			}
			attachmentResponse, err := srv.Users.Messages.Attachments.Get(user, mail.Id, attachment.Id).Do()
			if err != nil {
				return attachments, err
			}
			decodedContent, err := decodeAttachment(attachmentResponse.Data)
			if err != nil {
				return attachments, err
			}
			attachment.Content = decodedContent
			attachments[pos] = attachment
		}
	}

	return attachments, nil
}

func writeFile(path string, a *Attachment) error {
	return ioutil.WriteFile(path, a.Content, 0755)
}

func initGmailService() (*gmail.Service, error) {
	home := getHomeFolder()
	credentialsFile := fmt.Sprintf("%s/credentials.json", home)
	b, err := ioutil.ReadFile(credentialsFile)
	if err != nil {
		log.Fatalf("Unable to read client secret file: %v", err)
	}

	// If modifying these scopes, delete your previously saved token.json.
	config, err := google.ConfigFromJSON(b, gmail.GmailReadonlyScope)
	if err != nil {
		log.Fatalf("Unable to parse client secret file to config: %v", err)
	}
	client := getClient(config)
	return gmail.New(client)
}

func downloadByLabel(label string, path string, notOverwrite bool) {
	srv, err := initGmailService()
	if err != nil {
		log.Fatalf("Unable to retrieve Gmail client: %v", err)
	}

	labelId, err := labelId(srv, label)
	if err != nil {
		log.Fatal(err)
	}

	messages, err := messagesByLabel(srv, labelId)
	if err != nil {
		log.Fatal(err)
	}

	for _, m := range messages {
		attachments, err := attachments(srv, m, path, notOverwrite)
		if err != nil {
			log.Fatal(err)
		}
		for _, a := range attachments {
			if a.Skip {
				log.Printf("notOverwrite option enabled, attachment %s already present, it should not be overwritten", fmt.Sprintf("%v/%v", path, a.Filename))
			} else {
				err = writeFile(fmt.Sprintf("%v/%v", path, a.Filename), &a)
				if err != nil {
					log.Fatal(err)
				}
			}
		}
	}
}
