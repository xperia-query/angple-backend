package v2

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/damoang/angple-backend/internal/common"
	v2repo "github.com/damoang/angple-backend/internal/repository/v2"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

const promotionCacheTTL = 5 * time.Minute
const promotionCacheKeyPrefix = "promo:insert:"

// PromotionHandler handles promotion post API endpoints
type PromotionHandler struct {
	promotionRepo v2repo.PromotionRepository
	redis         *redis.Client
}

// NewPromotionHandler creates a new PromotionHandler
func NewPromotionHandler(promotionRepo v2repo.PromotionRepository, redisClient ...*redis.Client) *PromotionHandler {
	h := &PromotionHandler{promotionRepo: promotionRepo}
	if len(redisClient) > 0 {
		h.redis = redisClient[0]
	}
	return h
}

// GetInsertPosts handles GET /api/v1/promotion/posts/insert?count=N
// Returns promotion posts to be inserted into board lists.
// Results are cached in Redis for 5 minutes to reduce DB load.
func (h *PromotionHandler) GetInsertPosts(c *gin.Context) {
	count := 3 // default
	if n, err := strconv.Atoi(c.Query("count")); err == nil && n > 0 && n <= 20 {
		count = n
	}

	// Try Redis cache first
	if h.redis != nil {
		cacheKey := fmt.Sprintf("%s%d", promotionCacheKeyPrefix, count)
		cached, err := h.redis.Get(context.Background(), cacheKey).Bytes()
		if err == nil {
			c.Data(http.StatusOK, "application/json", cached)
			return
		}
	}

	posts, err := h.promotionRepo.FindInsertPosts(count)
	if err != nil {
		common.V2ErrorResponse(c, http.StatusInternalServerError, "직홍 게시글 조회 실패", err)
		return
	}

	// Cache the JSON response
	if h.redis != nil {
		resp := common.V2Response{Success: true, Data: posts}
		if data, err := json.Marshal(resp); err == nil {
			cacheKey := fmt.Sprintf("%s%d", promotionCacheKeyPrefix, count)
			h.redis.Set(context.Background(), cacheKey, data, promotionCacheTTL)
		}
	}

	common.V2Success(c, posts)
}
