package cache

import "fmt"

// Key prefixes — namespace isolation between different data types.
const (
	PrefixUser       = "user:"
	PrefixMemory     = "mem:"
	PrefixMemoryList = "memlist:"
	PrefixNamespace  = "ns:"
	PrefixSearch     = "search:"
	PrefixEmbedding  = "embed:"
	PrefixRetrieval  = "retrieval:"
)

// UserKey returns a cache key for a user by ID.
func UserKey(userID string) string {
	return PrefixUser + userID
}

// MemoryKey returns a cache key for a single memory.
func MemoryKey(memoryID string) string {
	return PrefixMemory + memoryID
}

// MemoryListKey returns a cache key for a paginated memory list query.
func MemoryListKey(userID, namespace, memType, status string, page, pageSize int) string {
	return fmt.Sprintf("%s%s:%s:%s:%s:%d:%d",
		PrefixMemoryList, userID, namespace, memType, status, page, pageSize,
	)
}

// NamespaceSuggestKey returns a cache key for namespace suggestions.
func NamespaceSuggestKey(userID, prefix string) string {
	return fmt.Sprintf("%s%s:%s", PrefixNamespace, userID, prefix)
}

// SearchKey returns a cache key for a FTS search result.
func SearchKey(userID, query, namespace, memType string, page, pageSize int) string {
	return fmt.Sprintf("%s%s:%s:%s:%s:%d:%d",
		PrefixSearch, userID, query, namespace, memType, page, pageSize,
	)
}

// EmbeddingKey returns a cache key for an embedding vector.
func EmbeddingKey(text, model string) string {
	return fmt.Sprintf("%s%s:%s", PrefixEmbedding, model, text)
}

// RetrievalKey returns a cache key for a retrieval result.
func RetrievalKey(userID, query, namespace string, topK int) string {
	return fmt.Sprintf("%s%s:%s:%s:%d", PrefixRetrieval, userID, query, namespace, topK)
}

// InvalidateUser removes all cached data for a user.
// Call this after any write operation on user data.
func InvalidateUser(c Cache, userID string) {
	c.Delete(UserKey(userID))
	c.DeletePrefix(PrefixMemoryList + userID)
	c.DeletePrefix(PrefixSearch + userID)
	c.DeletePrefix(PrefixRetrieval + userID)
	c.DeletePrefix(PrefixNamespace + userID)
}

// InvalidateMemory removes cached data for a specific memory.
func InvalidateMemory(c Cache, memoryID, userID string) {
	c.Delete(MemoryKey(memoryID))
	// Also clear list caches for this user since a memory changed.
	c.DeletePrefix(PrefixMemoryList + userID)
	c.DeletePrefix(PrefixRetrieval + userID)
	c.DeletePrefix(PrefixSearch + userID)
}
