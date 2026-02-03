package api

import (
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
	_, err := httputils.LogRequestBody(r, h.logger)
	if err != nil {
		h.logger.Error("Failed to read request body: %v", err)
		httputils.HandleError(w, err)
		return
	}

	if err := httputils.ValidateMethod(r, http.MethodPost); err != nil {
		h.logger.Error("Method validation error: %v", err)
		httputils.HandleError(w, err)
		return
	}

	var payload WebhookPayload
	if err := httputils.DecodeJSON(r, &payload); err != nil {
		h.logger.Error("JSON decode error: %v", err)
		httputils.HandleError(w, err)
		return
	}

	documentID, err := h.extractDocumentID(payload.DocumentURL)
	if err != nil {
		h.logger.Error("Failed to extract document ID from URL '%s': %v", payload.DocumentURL, err)
		httputils.HandleError(w, err)
		return
	}

	h.logger.Info("Received webhook: event=%s, document_id=%d", payload.DocumentURL, documentID)

	document, err := h.paperless.GetDocument(documentID)
	if err != nil {
		h.logger.Error("Failed to fetch document %d: %v", documentID, err)
		httputils.HandleError(w, err)
		return
	}

	if err := h.Process(document); err != nil {
		h.logger.Error("Error processing webhook: %v", err)
		httputils.HandleError(w, err)
		return
	}

	if err := httputils.SuccessResponse(w, "Webhook processed successfully", nil); err != nil {
		h.logger.Error("Error sending response: %v", err)
	}
}

func (h *Handler) HandleProcessUntagged(w http.ResponseWriter, r *http.Request) {
	if err := httputils.ValidateMethod(r, http.MethodPost); err != nil {
		h.logger.Error("Method validation error: %v", err)
		httputils.HandleError(w, err)
		return
	}

	documents, err := h.paperless.GetDocumentsWithoutTags()
	if err != nil {
		h.logger.Error("Failed to fetch untagged documents: %v", err)
		httputils.HandleError(w, err)
		return
	}

	h.logger.Info("Found %d untagged documents to process", len(documents))

	var processed, failed int
	var failedIDs []int

	for _, document := range documents {
		h.logger.Info("Processing untagged document ID=%d", document.ID)

		if err := h.Process(&document); err != nil {
			h.logger.Error("Error processing untagged document ID=%d: %v", document.ID, err)

			failed++
			failedIDs = append(failedIDs, document.ID)
			continue
		}

		processed++
		h.logger.Info("Successfully processed untagged document ID=%d", document.ID)
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
		h.logger.Error("Error sending response: %v", err)
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

func (h *Handler) Process(document *paperless.Document) error {
	h.logger.Info("Processing document ID: %d", document.ID)
	h.logger.Debug("Document content preview: %.200s...", document.Content)

	estimatedTokens := processor.EstimateTokens(document.Content)
	shouldReduce := processor.ShouldReduceContent(estimatedTokens, h.cfg.Reduction.ThresholdTokens)

	h.logger.Info("Path decision: estimated_tokens=%d, threshold=%d, should_reduce=%v",
		estimatedTokens, h.cfg.Reduction.ThresholdTokens, shouldReduce)

	llmContent := document.Content

	if shouldReduce {
		h.logger.Info("LONG PATH selected: document requires reduction")
		llmContent = processor.ReduceContent(document.Content, &h.cfg.Reduction)
		h.logger.Debug("Reduced Content: %s", llmContent)
	}

	documentTypes, err := h.paperless.GetDocumentTypes()
	if err != nil {
		h.logger.Error("Failed to fetch document types: %v", err)
		return err
	}

	tags, err := h.paperless.GetTags()
	if err != nil {
		h.logger.Error("Failed to fetch tags: %v", err)
		return err
	}

	suggestedTags := make([]string, len(tags))
	for i, t := range tags {
		suggestedTags[i] = t.Name
	}

	if len(suggestedTags) > h.cfg.Semantic.TagsThreshold {
		suggestedTags, err = h.semanticMatcher.GetTagSuggestions(llmContent, suggestedTags)
		if err != nil {
			h.logger.Error("Failed to get semantic tag suggestions: %v", err)
			return err
		}
	}
	h.logger.Info("Semantic tag suggestions: %v", suggestedTags)

	result, err := h.llm.AnalyzeContent(
		llmContent,
		document.PageCount,
		documentTypes,
		suggestedTags,
	)
	if err != nil {
		h.logger.Error("LLM request failed: %v", err)
		return err
	}
	h.logger.Debug("Result: %v", result)

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

	createdTags, err := h.paperless.CreateTags(documentNewTags)
	if err != nil {
		h.logger.Error("Failed to create new tags: %v", err)
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

	correspondent, err := h.paperless.CreateCorrespondent(*utils.Truncate(&result.Author, 127))
	if err != nil {
		h.logger.Error("Failed to create new correspondent: %v", err)
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
		Title:         utils.Truncate(&result.Title, 127),
		Tags:          documentTagsIds,
		DocumentType:  &documentType,
		Correspondent: &correspondent.ID,
	}

	err = h.paperless.UpdateDocument(document.ID, &updatedDocument)
	if err != nil {
		h.logger.Error("Failed to update document %d: %v", document.ID, err)
		return err
	}

	h.logger.Info(
		"Document updated successfully: Title='%s', Tags=%v, DocumentType=%v, Correspondent=%v",
		*updatedDocument.Title,
		updatedDocument.Tags,
		*updatedDocument.DocumentType,
		*updatedDocument.Correspondent,
	)
	return nil
}
