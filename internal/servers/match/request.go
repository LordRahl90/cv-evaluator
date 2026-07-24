package match

type ByDescriptionRequest struct {
	Description string `json:"description" binding:"required"`
}
