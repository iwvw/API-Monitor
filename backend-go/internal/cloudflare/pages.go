package cloudflare

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/iwvw/api-monitor/backend-go/internal/response"
)

func (s *Service) pages(w http.ResponseWriter, r *http.Request, accountID string) {
	if r.Method != http.MethodGet {
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	auth, ok := s.authForAccount(w, r, accountID)
	if !ok {
		return
	}
	cfAccountID, err := s.cloudflareAccountID(r.Context(), auth)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	payload, err := s.cfRequest(r.Context(), http.MethodGet, "/accounts/"+url.PathEscape(cfAccountID)+"/pages/projects", auth, nil)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	projects := []map[string]interface{}{}
	for _, item := range arrayValue(payload["result"]) {
		projects = append(projects, mapPagesProject(objectValue(item)))
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"projects": projects, "cfAccountId": cfAccountID})
}

func (s *Service) pagesProject(w http.ResponseWriter, r *http.Request, accountID, projectName string) {
	if r.Method != http.MethodDelete {
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	auth, ok := s.authForAccount(w, r, accountID)
	if !ok {
		return
	}
	cfAccountID, err := s.cloudflareAccountID(r.Context(), auth)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	if _, err := s.cfRequest(r.Context(), http.MethodDelete, "/accounts/"+url.PathEscape(cfAccountID)+"/pages/projects/"+url.PathEscape(projectName), auth, nil); err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

func (s *Service) pagesDeployments(w http.ResponseWriter, r *http.Request, accountID, projectName string) {
	if r.Method != http.MethodGet {
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	auth, ok := s.authForAccount(w, r, accountID)
	if !ok {
		return
	}
	cfAccountID, err := s.cloudflareAccountID(r.Context(), auth)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	path := "/accounts/" + url.PathEscape(cfAccountID) + "/pages/projects/" + url.PathEscape(projectName) + "/deployments?per_page=20"
	payload, err := s.cfRequest(r.Context(), http.MethodGet, path, auth, nil)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	deployments := []map[string]interface{}{}
	for _, item := range arrayValue(payload["result"]) {
		deployment := objectValue(item)
		if len(deployment) == 0 {
			continue
		}
		deployments = append(deployments, mapPagesDeployment(deployment))
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "deployments": deployments})
}

func (s *Service) deletePagesDeployment(w http.ResponseWriter, r *http.Request, accountID, projectName, deploymentID string) {
	if r.Method != http.MethodDelete {
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	auth, ok := s.authForAccount(w, r, accountID)
	if !ok {
		return
	}
	cfAccountID, err := s.cloudflareAccountID(r.Context(), auth)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	path := "/accounts/" + url.PathEscape(cfAccountID) + "/pages/projects/" + url.PathEscape(projectName) + "/deployments/" + url.PathEscape(deploymentID)
	if _, err := s.cfRequest(r.Context(), http.MethodDelete, path, auth, nil); err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

func (s *Service) pagesDomains(w http.ResponseWriter, r *http.Request, accountID, projectName string) {
	auth, ok := s.authForAccount(w, r, accountID)
	if !ok {
		return
	}
	cfAccountID, err := s.cloudflareAccountID(r.Context(), auth)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	path := "/accounts/" + url.PathEscape(cfAccountID) + "/pages/projects/" + url.PathEscape(projectName) + "/domains"
	switch r.Method {
	case http.MethodGet:
		payload, err := s.cfRequest(r.Context(), http.MethodGet, path, auth, nil)
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
			return
		}
		domains := []map[string]interface{}{}
		for _, item := range arrayValue(payload["result"]) {
			domains = append(domains, mapPagesDomain(objectValue(item)))
		}
		response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "domains": domains})
	case http.MethodPost:
		payload, err := readObject(r)
		if err != nil {
			response.JSON(w, http.StatusBadRequest, map[string]interface{}{"error": err.Error()})
			return
		}
		domain := strings.TrimSpace(stringValue(payload["domain"], ""))
		if domain == "" {
			response.JSON(w, http.StatusBadRequest, map[string]interface{}{"error": "domain is required"})
			return
		}
		result, err := s.cfRequest(r.Context(), http.MethodPost, path, auth, map[string]interface{}{"name": domain})
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
			return
		}
		response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "domain": result["result"]})
	default:
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Service) deletePagesDomain(w http.ResponseWriter, r *http.Request, accountID, projectName, domain string) {
	if r.Method != http.MethodDelete {
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	auth, ok := s.authForAccount(w, r, accountID)
	if !ok {
		return
	}
	cfAccountID, err := s.cloudflareAccountID(r.Context(), auth)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	path := "/accounts/" + url.PathEscape(cfAccountID) + "/pages/projects/" + url.PathEscape(projectName) + "/domains/" + url.PathEscape(domain)
	if _, err := s.cfRequest(r.Context(), http.MethodDelete, path, auth, nil); err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

func mapPagesProject(project map[string]interface{}) map[string]interface{} {
	var latest interface{}
	if deployment := objectValue(project["latest_deployment"]); len(deployment) > 0 {
		stage := objectValue(deployment["latest_stage"])
		latest = map[string]interface{}{
			"id":        deployment["id"],
			"url":       deployment["url"],
			"status":    stringValue(stage["status"], "unknown"),
			"createdOn": deployment["created_on"],
		}
	}
	return map[string]interface{}{
		"name":             project["name"],
		"subdomain":        project["subdomain"],
		"domains":          arrayValue(project["domains"]),
		"createdOn":        project["created_on"],
		"productionBranch": project["production_branch"],
		"latestDeployment": latest,
	}
}

func mapPagesDeployment(deployment map[string]interface{}) map[string]interface{} {
	stage := objectValue(deployment["latest_stage"])
	return map[string]interface{}{
		"id":          deployment["id"],
		"url":         deployment["url"],
		"environment": deployment["environment"],
		"status":      stringValue(stage["status"], "unknown"),
		"createdOn":   deployment["created_on"],
		"source":      deployment["source"],
		"buildConfig": deployment["build_config"],
	}
}

func mapPagesDomain(domain map[string]interface{}) map[string]interface{} {
	validation := objectValue(domain["validation_data"])
	return map[string]interface{}{
		"id":               domain["id"],
		"name":             domain["name"],
		"status":           domain["status"],
		"validationStatus": nullableString(stringValue(validation["status"], "")),
		"createdOn":        domain["created_on"],
	}
}
