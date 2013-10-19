// Copyright 2013 wetalk authors
//
// Licensed under the Apache License, Version 2.0 (the "License"): you may
// not use this file except in compliance with the License. You may obtain
// a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS, WITHOUT
// WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the
// License for the specific language governing permissions and limitations
// under the License.

// Package routers implemented controller methods of beego.
package routers

import (
	"fmt"
	"html/template"
	"net/url"
	"reflect"
	"strings"
	"time"

	"github.com/astaxie/beego"
	"github.com/astaxie/beego/validation"
	"github.com/beego/i18n"

	"github.com/beego/wetalk/models"
	"github.com/beego/wetalk/utils"
)

type NestPreparer interface {
	NestPrepare()
}

// baseRouter implemented global settings for all other routers.
type baseRouter struct {
	beego.Controller
	i18n.Locale
	user    models.User
	isLogin bool
}

// Prepare implemented Prepare method for baseRouter.
func (this *baseRouter) Prepare() {
	// if the host not matching app settings then redirect to AppUrl
	if this.Ctx.Request.Host != utils.AppHost {
		this.Redirect(utils.AppUrl, 302)
		return
	}

	// page start time
	this.Data["PageStartTime"] = time.Now()

	// start session
	this.StartSession()

	// check flash redirect, if match url then end, else for redirect return
	if match, redir := this.CheckFlashRedirect(this.Ctx.Request.RequestURI); redir {
		return
	} else if match {
		this.EndFlashRedirect()
	}

	// save logined user if exist in session
	if models.GetUserFromSession(&this.user, this.CruSession) {
		this.isLogin = true
		this.Data["User"] = &this.user
		this.Data["IsLogin"] = this.isLogin

		// if user forbided then do logout
		if this.user.IsForbid {
			models.LogoutUser(&this.Controller)
			this.FlashRedirect("/login", 302, "UserForbid")
			return
		}

	} else {
		this.isLogin = false
	}

	// Setting properties.
	this.Data["AppDescription"] = utils.AppDescription
	this.Data["AppKeywords"] = utils.AppKeywords
	this.Data["AppName"] = utils.AppName
	this.Data["AppVer"] = utils.AppVer
	this.Data["AppUrl"] = utils.AppUrl
	this.Data["AppLogo"] = utils.AppLogo
	this.Data["AvatarURL"] = utils.AvatarURL
	this.Data["IsProMode"] = utils.IsProMode
	this.Data["DateFormat"] = utils.DateFormat
	this.Data["DateTimeFormat"] = utils.DateTimeFormat

	// Redirect to make URL clean.
	if this.setLang() {
		i := strings.Index(this.Ctx.Request.RequestURI, "?")
		this.Redirect(this.Ctx.Request.RequestURI[:i], 302)
		return
	}

	// read flash message
	beego.ReadFromRequest(&this.Controller)

	// pass xsrf helper to template context
	xsrfToken := this.XsrfToken()
	this.Data["xsrf_token"] = xsrfToken
	this.Data["xsrf_html"] = template.HTML(this.XsrfFormHtml())

	// if method is GET then auto create a form once token
	if this.Ctx.Request.Method == "GET" {
		this.FormOnceCreate()
	}

	if app, ok := this.AppController.(NestPreparer); ok {
		app.NestPrepare()
	}
}

// on router finished
func (this *baseRouter) Finish() {

}

// check if user not active then redirect
func (this *baseRouter) CheckActiveRedirect(args ...interface{}) bool {
	var redirect_to string
	code := 302
	needActive := true
	for _, arg := range args {
		switch v := arg.(type) {
		case bool:
			needActive = v
		case string:
			// custom redirect url
			redirect_to = v
		case int:
			code = v
		}
	}
	if needActive {
		// redirect to active page
		if !this.user.IsActive {
			this.FlashRedirect("/settings/profile", code, "NeedActive")
			return true
		}
	} else {
		// no need active
		if this.user.IsActive {
			if redirect_to == "" {
				redirect_to = "/"
			}
			this.Redirect(redirect_to, code)
			return true
		}
	}
	return false

}

// check if not login then redirect
func (this *baseRouter) CheckLoginRedirect(args ...interface{}) bool {
	var redirect_to string
	code := 302
	needLogin := true
	for _, arg := range args {
		switch v := arg.(type) {
		case bool:
			needLogin = v
		case string:
			// custom redirect url
			redirect_to = v
		case int:
			// custom redirect url
			code = v
		}
	}

	// if need login then redirect
	if needLogin && !this.isLogin {
		if len(redirect_to) == 0 {
			req := this.Ctx.Request
			scheme := "http"
			if req.TLS != nil {
				scheme += "s"
			}
			redirect_to = fmt.Sprintf("%s://%s%s", scheme, req.Host, req.RequestURI)
		}
		redirect_to = "/login?to=" + url.QueryEscape(redirect_to)
		this.Redirect(redirect_to, code)
		return true
	}

	// if not need login then redirect
	if !needLogin && this.isLogin {
		if len(redirect_to) == 0 {
			redirect_to = "/"
		}
		this.Redirect(redirect_to, code)
		return true
	}
	return false
}

// read beego flash message
func (this *baseRouter) FlashRead(key string) (string, bool) {
	if data, ok := this.Data["flash"].(map[string]string); ok {
		value, ok := data[key]
		return value, ok
	}
	return "", false
}

// write beego flash message
func (this *baseRouter) FlashWrite(key string, value string) {
	flash := beego.NewFlash()
	flash.Data[key] = value
	flash.Store(&this.Controller)
}

// check flash redirect, ensure browser redirect to uri and display flash message.
func (this *baseRouter) CheckFlashRedirect(value string) (match bool, redirect bool) {
	v := this.GetSession("on_redirect")
	if params, ok := v.([]interface{}); ok {
		if len(params) != 5 {
			this.EndFlashRedirect()
			goto end
		}
		uri := utils.ToStr(params[0])
		code := 302
		if c, ok := params[1].(int); ok {
			if c/100 == 3 {
				code = c
			}
		}
		flag := utils.ToStr(params[2])
		flagVal := utils.ToStr(params[3])
		times := 0
		if v, ok := params[4].(int); ok {
			times = v
		}

		times += 1
		if times > 3 {
			// if max retry times reached then end
			this.EndFlashRedirect()
			goto end
		}

		// match uri or flash flag
		if uri == value || flag == value {
			match = true
		} else {
			// if no match then continue redirect
			this.FlashRedirect(uri, code, flag, flagVal, times)
			redirect = true
		}
	}
end:
	return match, redirect
}

// set flash redirect
func (this *baseRouter) FlashRedirect(uri string, code int, flag string, args ...interface{}) {
	flagVal := "true"
	times := 0
	for _, arg := range args {
		switch v := arg.(type) {
		case string:
			flagVal = v
		case int:
			times = v
		}
	}

	if len(uri) == 0 || uri[0] != '/' {
		panic("flash reirect only support same host redirect")
	}

	params := []interface{}{uri, code, flag, flagVal, times}
	this.SetSession("on_redirect", params)

	this.FlashWrite(flag, flagVal)
	this.Redirect(uri, code)
}

// clear flash redirect
func (this *baseRouter) EndFlashRedirect() {
	this.DelSession("on_redirect")
}

// check form once, void re-submit
func (this *baseRouter) FormOnceNotMatch() bool {
	notMatch := false
	recreat := false

	// get token from request param / header
	var value string
	if vus, ok := this.Input()["_once"]; ok && len(vus) > 0 {
		value = vus[0]
	} else {
		value = this.Ctx.Input.Header("X-Form-Once")
	}

	// exist in session
	if v, ok := this.GetSession("form_once").(string); ok && v != "" {
		// not match
		if value != v {
			notMatch = true
		} else {
			// if matched then re-creat once
			recreat = true
		}
	}

	this.FormOnceCreate(recreat)
	return notMatch
}

// create form once html
func (this *baseRouter) FormOnceCreate(args ...bool) {
	var value string
	var creat bool
	creat = len(args) > 0 && args[0]
	if !creat {
		if v, ok := this.GetSession("form_once").(string); ok && v != "" {
			value = v
		} else {
			creat = true
		}
	}
	if creat {
		value = utils.GetRandomString(10)
		this.SetSession("form_once", value)
	}
	this.Data["once_token"] = value
	this.Data["once_html"] = template.HTML(`<input type="hidden" name="_once" value="` + value + `">`)
}

func (this *baseRouter) validForm(form interface{}, names ...string) (bool, map[string]*validation.ValidationError) {
	// parse request params to form ptr struct
	utils.ParseForm(form, this.Input())

	// Put data back in case users input invalid data for any section.
	name := reflect.ValueOf(form).Elem().Type().Name()
	if len(names) > 0 {
		name = names[0]
	}
	this.Data[name] = form

	errName := name + "Error"

	// check form once
	if this.FormOnceNotMatch() {
		return false, nil
	}

	// Verify basic input.
	valid := validation.Validation{}
	if ok, _ := valid.Valid(form); !ok {
		errs := valid.ErrorMap()
		this.Data[errName] = &valid
		return false, errs
	}
	return true, nil
}

// valid form and put errors to tempalte context
func (this *baseRouter) ValidForm(form interface{}, names ...string) bool {
	valid, _ := this.validForm(form, names...)
	return valid
}

// valid form and put errors to tempalte context
func (this *baseRouter) ValidFormSets(form interface{}, names ...string) bool {
	valid, errs := this.validForm(form, names...)
	this.setFormSets(form, errs, names...)
	return valid
}

func (this *baseRouter) SetFormSets(form interface{}, names ...string) *utils.FormSets {
	return this.setFormSets(form, nil, names...)
}

func (this *baseRouter) setFormSets(form interface{}, errs map[string]*validation.ValidationError, names ...string) *utils.FormSets {
	formSets := utils.NewFormSets(form, errs, this.Locale)
	name := reflect.ValueOf(form).Elem().Type().Name()
	if len(names) > 0 {
		name = names[0]
	}
	name += "Sets"
	this.Data[name] = formSets

	return formSets
}

// add valid error to FormError
func (this *baseRouter) SetFormError(form interface{}, fieldName, errMsg string, names ...string) {
	name := reflect.ValueOf(form).Elem().Type().Name()
	if len(names) > 0 {
		name = names[0]
	}
	errName := name + "Error"
	setsName := name + "Sets"

	if valid, ok := this.Data[errName].(*validation.Validation); ok {
		valid.SetError(fieldName, this.Tr(errMsg))
	}

	if fSets, ok := this.Data[setsName].(*utils.FormSets); ok {
		fSets.SetError(fieldName, errMsg)
	}
}

// check xsrf and show a friendly page
func (this *baseRouter) CheckXsrfCookie() bool {
	return this.Controller.CheckXsrfCookie()
}

func (this *baseRouter) SystemException() {

}

func (this *baseRouter) IsAjax() bool {
	return this.Ctx.Input.Header("X-Requested-With") == "XMLHttpRequest"
}

func (this *baseRouter) SetPaginator(per int, nums int64) *utils.Paginator {
	p := utils.NewPaginator(this.Ctx.Request, per, nums)
	this.Data["paginator"] = p
	return p
}

// setLang sets site language version.
func (this *baseRouter) setLang() bool {
	isNeedRedir := false

	// get all lang names from i18n
	langs := i18n.ListLangs()

	// 1. Check URL arguments.
	lang := this.GetString("lang")

	// 2. Get language information from cookies.
	if len(lang) == 0 {
		lang = this.Ctx.GetCookie("lang")
	} else {
		isNeedRedir = true
	}

	// Check again in case someone modify by purpose.
	if !i18n.IsExist(lang) {
		lang = ""
		isNeedRedir = false
	}

	// 3. Get language information from 'Accept-Language'.
	if len(lang) == 0 {
		al := this.Ctx.Input.Header("Accept-Language")
		if len(al) > 4 {
			al = al[:5] // Only compare first 5 letters.
			if i18n.IsExist(al) {
				lang = al
			}
		}
	}

	// 4. DefaucurLang language is English.
	if len(lang) == 0 {
		lang = "en-US"
		isNeedRedir = false
	}

	// Save language information in cookies.
	this.Ctx.SetCookie("lang", lang, 1<<31-1, "/")

	// Set language properties.
	this.Data["Lang"] = lang
	this.Data["Langs"] = langs

	this.Lang = lang

	return isNeedRedir
}
