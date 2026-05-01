// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package handler

import (
	"net/http"

	"github.com/zeromicro/go-zero/rest/httpx"
	"nof0-api/internal/logic"
	"nof0-api/internal/svc"
	"nof0-api/internal/types"
)

func TradersHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.TradersRequest
		if err := httpx.Parse(r, &req); err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
			return
		}
		l := logic.NewTradersLogic(r.Context(), svcCtx)
		resp, err := l.Traders(&req)
		if err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
		} else {
			httpx.OkJsonCtx(r.Context(), w, resp)
		}
	}
}

func TraderDetailHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.TraderPathRequest
		if err := httpx.Parse(r, &req); err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
			return
		}
		l := logic.NewTraderDetailLogic(r.Context(), svcCtx)
		resp, err := l.TraderDetail(&req)
		if err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
		} else {
			httpx.OkJsonCtx(r.Context(), w, resp)
		}
	}
}

func TraderStatusHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.TraderPathRequest
		if err := httpx.Parse(r, &req); err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
			return
		}
		l := logic.NewTraderStatusLogic(r.Context(), svcCtx)
		resp, err := l.TraderStatus(&req)
		if err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
		} else {
			httpx.OkJsonCtx(r.Context(), w, resp)
		}
	}
}

func AuditEventsHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.AuditEventsRequest
		if err := httpx.Parse(r, &req); err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
			return
		}
		l := logic.NewAuditEventsLogic(r.Context(), svcCtx)
		resp, err := l.AuditEvents(&req)
		if err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
		} else {
			httpx.OkJsonCtx(r.Context(), w, resp)
		}
	}
}

func OrdersHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.OrdersRequest
		if err := httpx.Parse(r, &req); err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
			return
		}
		l := logic.NewOrdersLogic(r.Context(), svcCtx)
		resp, err := l.Orders(&req)
		if err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
		} else {
			httpx.OkJsonCtx(r.Context(), w, resp)
		}
	}
}

func TraderStartHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return traderControlHandler(svcCtx, "start")
}

func TraderStopHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return traderControlHandler(svcCtx, "stop")
}

func TraderPauseHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return traderControlHandler(svcCtx, "pause")
}

func TraderResumeHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return traderControlHandler(svcCtx, "resume")
}

func traderControlHandler(svcCtx *svc.ServiceContext, action string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.TraderControlRequest
		if err := httpx.Parse(r, &req); err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
			return
		}
		l := logic.NewTraderControlLogic(r.Context(), svcCtx)
		resp, err := l.Control(&req, action)
		if err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
		} else {
			httpx.OkJsonCtx(r.Context(), w, resp)
		}
	}
}

func DecisionApproveHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return decisionActionHandler(svcCtx, "approve")
}

func DecisionRejectHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return decisionActionHandler(svcCtx, "reject")
}

func decisionActionHandler(svcCtx *svc.ServiceContext, action string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.DecisionActionRequest
		if err := httpx.Parse(r, &req); err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
			return
		}
		l := logic.NewDecisionActionLogic(r.Context(), svcCtx)
		resp, err := l.DecisionAction(&req, action)
		if err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
		} else {
			httpx.OkJsonCtx(r.Context(), w, resp)
		}
	}
}

func OrderPreviewHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.OrderPreviewRequest
		if err := httpx.Parse(r, &req); err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
			return
		}
		l := logic.NewOrderPreviewLogic(r.Context(), svcCtx)
		resp, err := l.OrderPreview(&req)
		if err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
		} else {
			httpx.OkJsonCtx(r.Context(), w, resp)
		}
	}
}

func OrderApproveHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return orderActionHandler(svcCtx, "approve")
}

func OrderRejectHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return orderActionHandler(svcCtx, "reject")
}

func orderActionHandler(svcCtx *svc.ServiceContext, action string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.OrderActionRequest
		if err := httpx.Parse(r, &req); err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
			return
		}
		l := logic.NewOrderActionLogic(r.Context(), svcCtx)
		resp, err := l.OrderAction(&req, action)
		if err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
		} else {
			httpx.OkJsonCtx(r.Context(), w, resp)
		}
	}
}
