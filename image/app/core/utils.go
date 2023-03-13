// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package core

import (
	"fmt"
	"integration/app/config"
	"integration/app/logging"
	"net/http"
	"net/smtp"
)

func GetUserFromHeader(h http.Header) string {
	return getValueFromHeader(h, "Ajp_uid")
}

func GetShibSessionFromHeader(h http.Header) string {
	return getValueFromHeader(h, "Ajp_shib-Session-Id")
}

func getValueFromHeader(h http.Header, hn string) string {
	if config.GetConfig().Options.UserHeaderName != "" {
		hn = config.GetConfig().Options.UserHeaderName
	}
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

func getSubjectOnSucces(job Job) string {
	return fmt.Sprintf("[rdm-integration] Done updating files in dataset %v", job.PersistentId)
}

func getContentOnSucces(job Job) string {
	return fmt.Sprintf("All files are updated sucessfuly. You can review the content and edit the metadata in the dataset: "+
		"<a href=\"%v\">%v</a>.", Destination.GetRepoUrl(job.PersistentId, true), job.PersistentId)
}

func getSubjectOnError(_ error, job Job) string {
	return fmt.Sprintf("[rdm-integration] Failed updating files in dataset %v", job.PersistentId)
}

func getContentOnError(errIn error, job Job) string {
	return fmt.Sprintf("Updating files in dataset <a href=\"%v\">%v</a> has failed with the following error: "+
		"%v<br><br>Please try again later. If the error persists, contact the helpdesk.",
		Destination.GetRepoUrl(job.PersistentId, true), job.PersistentId, errIn)
}
