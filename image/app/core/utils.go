// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package core

import (
	"context"
	"fmt"
	"integration/app/config"
	"integration/app/logging"
	"net/http"
	"net/smtp"

	"github.com/google/uuid"
	"github.com/libis/rdm-dataverse-go-api/api"
)

func GetUserFromHeader(h http.Header) string {
	token := getValueFromHeader(h, "X-Forwarded-Access-Token")
	if token != "" {
		client := api.NewClient(config.GetConfig().DataverseServer)
		header := http.Header{"Authorization": []string{"Bearer " + token}}
		res := api.User{}
		api.Do(context.Background(), client.NewRequest("/api/v1/users/:me", "GET", nil, header), &res)
		if len(res.Data.Identifier) > 1 {
			return res.Data.Identifier[1:]
		} else {
			return ""
		}
	}
	hn := "Ajp_uid"
	if config.GetConfig().Options.UserHeaderName != "" {
		hn = config.GetConfig().Options.UserHeaderName
	}
	return getValueFromHeader(h, hn)
}

func GetSessionId(h http.Header) string {
	fromHeader := getValueFromHeader(h, "Ajp_shib-Session-Id")
	if fromHeader == "" {
		return uuid.NewString()
	}
	return fromHeader
}

func getValueFromHeader(h http.Header, hn string) string {
	return h.Get(hn)
}

func SendMail(msg string, to []string) error {
	if config.GetConfig().Options.SmtpConfig.Host == "" {
		logging.Logger.Println("smtp is not configured: message could not be sent:", msg)
		return nil
	}
	conf := config.GetConfig().Options.SmtpConfig
	var auth smtp.Auth
	if config.SmtpPassword != "" {
		auth = smtp.PlainAuth("", conf.From, config.SmtpPassword, conf.Host)
	}
	return smtp.SendMail(conf.Host+":"+conf.Port, auth, conf.From, to, []byte(msg))
}

func getSubjectOnSuccess(job Job) string {
	template := "[rdm-integration] Done uploading files to dataset %v"
	if config.GetConfig().Options.MailConfig.SubjectOnSuccess != "" {
		template = config.GetConfig().Options.MailConfig.SubjectOnSuccess
	}
	return fmt.Sprintf(template, job.PersistentId)
}

func getContentOnSuccess(job Job) string {
	template := "All files are updated successfully. You can review the content and edit the metadata in the dataset: <a href=\"%v\">%v</a>."
	if config.GetConfig().Options.MailConfig.ContentOnSuccess != "" {
		template = config.GetConfig().Options.MailConfig.ContentOnSuccess
	}
	return fmt.Sprintf(template, Destination.GetRepoUrl(job.PersistentId, true), job.PersistentId)
}

func getSubjectOnError(_ error, job Job) string {
	template := "[rdm-integration] Failed to upload all files to dataset %v"
	if config.GetConfig().Options.MailConfig.SubjectOnError != "" {
		template = config.GetConfig().Options.MailConfig.SubjectOnError
	}
	return fmt.Sprintf(template, job.PersistentId)
}

func getContentOnError(_ error, job Job) string {
	template := "Updating files in dataset <a href=\"%v\">%v</a> has failed. Please try again later. If the error persists, contact the helpdesk."
	if config.GetConfig().Options.MailConfig.ContentOnError != "" {
		template = config.GetConfig().Options.MailConfig.ContentOnError
	}
	return fmt.Sprintf(template, Destination.GetRepoUrl(job.PersistentId, true), job.PersistentId)
}
