package api

import (
	"bytes"
	"io"
	"net/http"
	"path"
	"slices"
	"strconv"
	"strings"

	"github.com/wgomg/itzamna/internal/config"
	"github.com/wgomg/itzamna/internal/llm"
	"github.com/wgomg/itzamna/internal/paperless"
	"github.com/wgomg/itzamna/internal/processor"
	"github.com/wgomg/itzamna/internal/semantic"
	"github.com/wgomg/itzamna/internal/utils"
	"github.com/wgomg/itzamna/internal/utils/httputils"
)

type Handler struct {
	logger          *utils.Logger
	paperless       *paperless.Client
	llm             *llm.Client
	semanticMatcher semantic.Matcher
	cfg             *config.Config
}

func NewHandler(
	logger *utils.Logger,
	paperless *paperless.Client,
	llm *llm.Client,
	semanticMatcher semantic.Matcher,
	cfg *config.Config,
) *Handler {
	return &Handler{
		logger:          logger,
		paperless:       paperless,
		llm:             llm,
		semanticMatcher: semanticMatcher,
		cfg:             cfg,
	}
}

func (h *Handler) HandleWebhook(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	reqID := ctx.Value("reqid").(string)

	bodyBytes, err := httputils.LogRequestBody(r, h.logger, reqID)
	if err != nil {
		h.logger.Error(&reqID, "Failed to read request body: %v", err)
		httputils.HandleError(w, err)
		return
	}
	r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

	if err := httputils.ValidateMethod(r, http.MethodPost); err != nil {
		h.logger.Error(&reqID, "Method validation error: %v", err)
		httputils.HandleError(w, err)
		return
	}

	var payload WebhookPayload
	if err := httputils.DecodeJSON(r, &payload); err != nil {
		h.logger.Error(&reqID, "JSON decode error: %v", err)
		httputils.HandleError(w, err)
		return
	}

	documentID, err := h.extractDocumentID(payload.DocumentURL)
	if err != nil {
		h.logger.Error(
			&reqID,
			"Failed to extract document ID from URL '%s': %v",
			payload.DocumentURL,
			err,
		)
		httputils.HandleError(w, err)
		return
	}

	h.logger.Info(
		&reqID,
		"Received webhook: event=%s, document_id=%d",
		payload.DocumentURL,
		documentID,
	)

	document, err := h.paperless.GetDocument(documentID, reqID)
	if err != nil {
		h.logger.Error(&reqID, "Failed to fetch document %d: %v", documentID, err)
		httputils.HandleError(w, err)
		return
	}

	if err := h.Process(document, reqID); err != nil {
		h.logger.Error(&reqID, "Error processing webhook: %v", err)
		httputils.HandleError(w, err)
		return
	}

	if err := httputils.SuccessResponse(w, "Webhook processed successfully", nil); err != nil {
		h.logger.Error(&reqID, "Error sending response: %v", err)
	}
}

func (h *Handler) HandleProcessUntagged(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	reqID := ctx.Value("reqid").(string)

	if err := httputils.ValidateMethod(r, http.MethodPost); err != nil {
		h.logger.Error(&reqID, "Method validation error: %v", err)
		httputils.HandleError(w, err)
		return
	}

	documents, err := h.paperless.GetDocumentsWithoutTags(reqID)
	if err != nil {
		h.logger.Error(&reqID, "Failed to fetch untagged documents: %v", err)
		httputils.HandleError(w, err)
		return
	}

	h.logger.Info(&reqID, "Found %d untagged documents to process", len(documents))

	var processed, failed int
	var failedIDs []int

	for _, document := range documents {
		h.logger.Info(&reqID, "Processing untagged document ID=%d", document.ID)

		if err := h.Process(&document, reqID); err != nil {
			h.logger.Error(&reqID, "Error processing untagged document ID=%d: %v", document.ID, err)

			failed++
			failedIDs = append(failedIDs, document.ID)
			continue
		}

		processed++
		h.logger.Info(&reqID, "Successfully processed untagged document ID=%d", document.ID)
	}

	response := map[string]interface{}{
		"status":    "completed",
		"total":     len(documents),
		"processed": processed,
		"failed":    failed,
	}

	if failed > 0 {
		response["failed_document_ids"] = failedIDs
	}

	if err := httputils.SuccessResponse(w, "Untagged documents processing completed", response); err != nil {
		h.logger.Error(&reqID, "Error sending response: %v", err)
	}
}

func (h *Handler) extractDocumentID(documentURL string) (int, error) {
	cleanPath := strings.Trim(documentURL, "/")
	base := path.Base(cleanPath)

	documentID, err := strconv.Atoi(base)
	if err != nil {
		return 0, err
	}

	return documentID, nil
}

func (h *Handler) Process(document *paperless.Document, reqID string) error {
	h.logger.Info(&reqID, "Processing document ID: %d", document.ID)
	h.logger.Debug(&reqID, "Document content preview: %.200s...", document.Content)

	estimatedTokens := processor.EstimateTokens(document.Content)
	shouldReduce := processor.ShouldReduceContent(estimatedTokens, h.cfg.Reduction.ThresholdTokens)

	h.logger.Info(&reqID, "Path decision: estimated_tokens=%d, threshold=%d, should_reduce=%v",
		estimatedTokens, h.cfg.Reduction.ThresholdTokens, shouldReduce)

	llmContent := document.Content

	if shouldReduce {
		h.logger.Info(&reqID, "LONG PATH selected: document requires reduction")
		llmContent = processor.ReduceContent(document.Content, &h.cfg.Reduction)
		h.logger.Debug(&reqID, "Reduced Content: %s", llmContent)
	}

	documentTypes, err := h.paperless.GetDocumentTypes(reqID)
	if err != nil {
		h.logger.Error(&reqID, "Failed to fetch document types: %v", err)
		return err
	}

	tags, err := h.paperless.GetTags(reqID)
	if err != nil {
		h.logger.Error(&reqID, "Failed to fetch tags: %v", err)
		return err
	}

	suggestedTags := make([]string, len(tags))
	for i, t := range tags {
		suggestedTags[i] = t.Name
	}

	if len(suggestedTags) > h.cfg.Semantic.TagsThreshold {
		suggestedTags, err = h.semanticMatcher.GetTagSuggestions(llmContent, suggestedTags, &reqID)
		if err != nil {
			h.logger.Error(&reqID, "Failed to get semantic tag suggestions: %v", err)
			return err
		}
	}
	h.logger.Info(&reqID, "Semantic tag suggestions: %v", suggestedTags)

	result, err := h.llm.AnalyzeContent(
		llmContent,
		document.PageCount,
		documentTypes,
		suggestedTags,
		reqID,
	)
	if err != nil {
		h.logger.Error(&reqID, "LLM request failed: %v", err)
		return err
	}
	h.logger.Debug(&reqID, "Result: %v", result)

	allTagsNames := make([]string, len(tags))
	for i, t := range tags {
		allTagsNames[i] = t.Name
	}

	var documentNewTags []string
	var documentExistingTags []string
	for _, t := range result.Tags {
		if !slices.Contains(allTagsNames, t) {
			documentNewTags = append(documentNewTags, t)
		} else {
			documentExistingTags = append(documentExistingTags, t)
		}
	}

	createdTags, err := h.paperless.CreateTags(documentNewTags, reqID)
	if err != nil {
		h.logger.Error(&reqID, "Failed to create new tags: %v", err)
		return err
	}
	var documentTagsIds []int
	for _, ct := range createdTags {
		documentTagsIds = append(documentTagsIds, ct.ID)
	}
	for _, t := range tags {
		if slices.Contains(documentExistingTags, t.Name) {
			documentTagsIds = append(documentTagsIds, t.ID)
		}
	}

	maxStringLength := 127

	correspondent, err := h.paperless.CreateCorrespondent(
		utils.Truncate(result.Author, maxStringLength), reqID,
	)
	if err != nil {
		h.logger.Error(&reqID, "Failed to create new correspondent: %v", err)
		return err
	}

	// verify returned document type matches pre-defined list, set to "other" if not
	documentType := 0
	for _, dt := range documentTypes {
		if dt.Name == result.Type {
			documentType = dt.ID
			break
		}

		if dt.Name == "other" && documentType == 0 {
			documentType = dt.ID
		}
	}

	updatedDocument := paperless.DocumentUpdate{
		Title:         utils.Truncate(result.Title, maxStringLength),
		Tags:          documentTagsIds,
		DocumentType:  documentType,
		Correspondent: correspondent.ID,
	}

	err = h.paperless.UpdateDocument(document.ID, &updatedDocument, reqID)
	if err != nil {
		h.logger.Error(&reqID, "Failed to update document %d: %v", document.ID, err)
		return err
	}

	h.logger.Info(&reqID,
		"Document updated successfully: Title='%s', Tags=%v, DocumentType=%v, Correspondent=%v",
		updatedDocument.Title,
		updatedDocument.Tags,
		updatedDocument.DocumentType,
		updatedDocument.Correspondent,
	)
	return nil
}
