// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package core

import (
	"integration/app/config"
	"net/http"
	"net/smtp"
)

func GetUserFromHeader(h http.Header) string {
	hn := "Ajp_uid"
	if config.GetConfig().Options.UserHeaderName != "" {
		hn = config.GetConfig().Options.UserHeaderName
	}
	return h.Get(hn)
}

func SendMail(msg string, to []string) error {
	if config.GetConfig().Options.SmtpConfig.Host == "" {
		return nil
	}
	conf := config.GetConfig().Options.SmtpConfig
	auth := smtp.PlainAuth("", conf.From, config.SmtpPassword, conf.Host)
	return smtp.SendMail(conf.Host+":"+conf.Port, auth, conf.From, to, []byte(msg))
}
