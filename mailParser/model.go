package main

import (
	"net/mail"
	"io"
)


type EmailUnderAnalysis struct {
	Filepath              string
	Header                mail.Header
	Body				  io.Reader
	HeaderParsingErrors   int
	ContentType           string
	HTMLBody              string
	TextBody              string
	BodyParsingErrors     int
	AttachmentsEmbeddings []AttachmentEmbedding
	BodyParts             []BodyPart
	//Content               io.Reader
}


type BodyPart struct {
	Id                         int
	ContentType                string
	ContentTransferEncoding    string
	ContentDisposition         string
	Filename                   string
	FormName                   string
	RawContentTransferEncoding []string
}


// Attachment with filename, content type and data (as a io.Reader)
type AttachmentEmbedding struct {
	Filename     string
	Details      FileDecode
	IsAttachment int
	IsEmbedding  int
}


type FileDecode struct {
	ContentType             string
	ContentTransferEncoding string
	ContentDisposition      string
	RealMimetype            string
	RealExtension           string
	Hash                    string
	Data                    []byte
}
