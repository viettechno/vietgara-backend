package handler

import (
	"context"

	documentv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/document/v1"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/viettechno/vietgara-backend/internal/document/printmodel"
	"github.com/viettechno/vietgara-backend/internal/document/usecase"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

// The RPCs of Release 13.1: the print agents and their jobs (FR-DOC-08).

// ListPrintAgents returns the garage's print agents.
func (s *Server) ListPrintAgents(ctx context.Context, req *documentv1.ListPrintAgentsRequest) (*documentv1.ListPrintAgentsResponse, error) {
	agents, err := s.uc.Agents(ctx, req.GetGarageId())
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	resp := &documentv1.ListPrintAgentsResponse{Agents: make([]*documentv1.PrintAgent, 0, len(agents))}
	for _, a := range agents {
		resp.Agents = append(resp.Agents, agentToProto(a))
	}

	return resp, nil
}

// CreatePrintAgent adds a print agent with its pairing code.
func (s *Server) CreatePrintAgent(ctx context.Context, req *documentv1.CreatePrintAgentRequest) (*documentv1.CreatePrintAgentResponse, error) {
	agent, code, err := s.uc.CreateAgent(ctx, req.GetGarageId(), req.GetAgent().GetName())
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return &documentv1.CreatePrintAgentResponse{Agent: agentToProto(agent), PairingCode: code}, nil
}

// IssuePrintAgentPairingCode gives an agent a new pairing code.
func (s *Server) IssuePrintAgentPairingCode(
	ctx context.Context, req *documentv1.IssuePrintAgentPairingCodeRequest,
) (*documentv1.CreatePrintAgentResponse, error) {
	agent, code, err := s.uc.IssuePairingCode(ctx, req.GetGarageId(), req.GetAgentId())
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return &documentv1.CreatePrintAgentResponse{Agent: agentToProto(agent), PairingCode: code}, nil
}

// DeletePrintAgent removes an agent.
func (s *Server) DeletePrintAgent(ctx context.Context, req *documentv1.DeletePrintAgentRequest) (*documentv1.DeletePrintAgentResponse, error) {
	if err := s.uc.DeleteAgent(ctx, req.GetGarageId(), req.GetAgentId()); err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return &documentv1.DeletePrintAgentResponse{}, nil
}

// CreatePrintJob prints a document on an agent's printer.
func (s *Server) CreatePrintJob(ctx context.Context, req *documentv1.CreatePrintJobRequest) (*documentv1.PrintJob, error) {
	doc, err := jobDocument(req)
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	job, err := s.uc.CreatePrintJob(ctx, req.GetGarageId(), req.GetPrinterId(), doc)
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return jobToProto(job), nil
}

// GetPrintJob returns a print job.
func (s *Server) GetPrintJob(ctx context.Context, req *documentv1.GetPrintJobRequest) (*documentv1.PrintJob, error) {
	job, err := s.uc.PrintJob(ctx, req.GetGarageId(), req.GetJobId())
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return jobToProto(job), nil
}

// AgentServer implements documentv1.PrintAgentServiceServer: the agent's
// pairing.
type AgentServer struct {
	documentv1.UnimplementedPrintAgentServiceServer

	uc *usecase.DocumentUseCase
}

// NewAgentServer wires the agent's API to the use case.
func NewAgentServer(uc *usecase.DocumentUseCase) *AgentServer {
	return &AgentServer{uc: uc}
}

// PairPrintAgent exchanges a pairing code for the agent's key.
func (s *AgentServer) PairPrintAgent(ctx context.Context, req *documentv1.PairPrintAgentRequest) (*documentv1.PairPrintAgentResponse, error) {
	result, err := s.uc.PairAgent(ctx, req.GetPairingCode(), usecase.AgentReport{
		Hostname: req.GetHostname(), OS: req.GetOs(), Version: req.GetVersion(),
	})
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return &documentv1.PairPrintAgentResponse{
		AgentKey: result.Key, AgentId: result.Agent.ID, AgentName: result.Agent.Name, GarageName: result.GarageName,
	}, nil
}

func jobDocument(req *documentv1.CreatePrintJobRequest) (usecase.JobDocument, error) {
	switch d := req.GetDocument().(type) {
	case *documentv1.CreatePrintJobRequest_QuoteId:
		return usecase.JobDocument{Type: printmodel.TypeQuote, ID: d.QuoteId}, nil
	case *documentv1.CreatePrintJobRequest_RepairOrderId:
		return usecase.JobDocument{Type: printmodel.TypeRepairOrder, ID: d.RepairOrderId}, nil
	case *documentv1.CreatePrintJobRequest_SettlementId:
		return usecase.JobDocument{Type: printmodel.TypeSettlement, ID: d.SettlementId}, nil
	case *documentv1.CreatePrintJobRequest_PaymentVoucher:
		return usecase.JobDocument{
			Type: printmodel.TypePaymentVoucher, ID: d.PaymentVoucher.GetSettlementId(), PaymentID: d.PaymentVoucher.GetPaymentId(),
		}, nil
	case *documentv1.CreatePrintJobRequest_GoodsReceiptId:
		return usecase.JobDocument{Type: printmodel.TypeGoodsReceipt, ID: d.GoodsReceiptId}, nil
	case *documentv1.CreatePrintJobRequest_StockIssueRepairOrderId:
		return usecase.JobDocument{Type: printmodel.TypeStockIssue, ID: d.StockIssueRepairOrderId}, nil
	case *documentv1.CreatePrintJobRequest_PaymentOutVoucherId:
		return usecase.JobDocument{Type: printmodel.TypePaymentOut, ID: d.PaymentOutVoucherId}, nil
	case *documentv1.CreatePrintJobRequest_PurchaseOrderId:
		return usecase.JobDocument{Type: printmodel.TypePurchaseOrder, ID: d.PurchaseOrderId}, nil
	case *documentv1.CreatePrintJobRequest_PartLabels:
		labels := make([]usecase.LabelCopies, 0, len(d.PartLabels.GetParts()))
		for _, p := range d.PartLabels.GetParts() {
			labels = append(labels, usecase.LabelCopies{PartID: p.GetPartId(), UnitID: p.GetUnitId(), Copies: int(p.GetCopies())})
		}

		return usecase.JobDocument{Type: printmodel.TypePartLabel, Labels: labels}, nil
	case *documentv1.CreatePrintJobRequest_TestPage:
		return usecase.JobDocument{TestPage: true}, nil
	}

	return usecase.JobDocument{}, apperror.Validation("document", apperror.IssueRequired, "name the document to print")
}

func agentToProto(a usecase.PrintAgent) *documentv1.PrintAgent {
	out := &documentv1.PrintAgent{
		Id: a.ID, Name: a.Name, Paired: a.Paired, Connected: a.Connected, Hostname: a.Hostname, Os: a.OS, Version: a.Version,
		CreatedAt: timestamppb.New(a.CreatedAt), UpdatedAt: timestamppb.New(a.UpdatedAt),
	}

	if a.PairingExpiresAt != nil {
		out.PairingExpiresAt = timestamppb.New(*a.PairingExpiresAt)
	}

	if a.LastSeenAt != nil {
		out.LastSeenAt = timestamppb.New(*a.LastSeenAt)
	}

	for _, p := range a.Printers {
		out.Printers = append(out.Printers, &documentv1.PrintAgentPrinter{Name: p.Name, IsDefault: p.Default})
	}

	return out
}

var jobStatusToProto = map[usecase.JobStatus]documentv1.PrintJobStatus{
	usecase.JobQueued:  documentv1.PrintJobStatus_PRINT_JOB_STATUS_QUEUED,
	usecase.JobSent:    documentv1.PrintJobStatus_PRINT_JOB_STATUS_SENT,
	usecase.JobPrinted: documentv1.PrintJobStatus_PRINT_JOB_STATUS_PRINTED,
	usecase.JobFailed:  documentv1.PrintJobStatus_PRINT_JOB_STATUS_FAILED,
	usecase.JobExpired: documentv1.PrintJobStatus_PRINT_JOB_STATUS_EXPIRED,
}

func jobToProto(j usecase.PrintJob) *documentv1.PrintJob {
	out := &documentv1.PrintJob{
		Id: j.ID, PrinterId: j.PrinterID, PrinterName: j.PrinterName, Status: jobStatusToProto[j.Status], Error: j.Error,
		CreatedAt: timestamppb.New(j.CreatedAt), UpdatedAt: timestamppb.New(j.UpdatedAt),
	}

	if j.DocumentType == printmodel.TypePartLabel {
		out.DocumentType = documentv1.DocumentType_DOCUMENT_TYPE_PART_LABEL
	} else if j.DocumentType != "" {
		out.DocumentType = documentTypeToProto[j.DocumentType]
	}

	return out
}
