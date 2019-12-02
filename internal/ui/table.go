package ui

import (
	"context"
	"errors"

	"github.com/derailed/k9s/internal/config"
	"github.com/derailed/k9s/internal/model"
	"github.com/derailed/k9s/internal/render"
	"github.com/derailed/tview"
	"github.com/gdamore/tcell"
	"github.com/rs/zerolog/log"
)

type (
	// ColorerFunc represents a row colorer.
	ColorerFunc func(ns string, evt render.RowEvent) tcell.Color

	// DecorateFunc represents a row decorator.
	DecorateFunc func(render.TableData) render.TableData

	// SelectedRowFunc a table selection callback.
	SelectedRowFunc func(r, c int)
)

// Table represents tabular data.
type Table struct {
	*SelectTable

	actions    KeyActions
	BaseTitle  string
	Path       string
	cmdBuff    *CmdBuff
	styles     *config.Styles
	sortCol    SortColumn
	sortFn     SortFn
	colorerFn  render.ColorerFunc
	decorateFn DecorateFunc
}

// NewTable returns a new table view.
func NewTable(title string) *Table {
	return &Table{
		SelectTable: &SelectTable{
			Table: tview.NewTable(),
			marks: make(map[string]bool),
		},
		actions:   make(KeyActions),
		cmdBuff:   NewCmdBuff('/', FilterBuff),
		BaseTitle: title,
		sortCol:   SortColumn{index: 0, colCount: 0, asc: true},
	}
}

func (t *Table) Init(ctx context.Context) {
	t.styles = mustExtractSyles(ctx)

	t.SetFixed(1, 0)
	t.SetBorder(true)
	t.SetBackgroundColor(config.AsColor(t.styles.Table().BgColor))
	t.SetBorderColor(config.AsColor(t.styles.Table().FgColor))
	t.SetBorderFocusColor(config.AsColor(t.styles.Frame().Border.FocusColor))
	t.SetBorderAttributes(tcell.AttrBold)
	t.SetBorderPadding(0, 0, 1, 1)
	t.SetSelectable(true, false)
	t.SetSelectedStyle(
		tcell.ColorBlack,
		config.AsColor(t.styles.Table().CursorColor),
		tcell.AttrBold,
	)
	t.SetSelectionChangedFunc(t.selChanged)
	t.SetInputCapture(t.keyboard)
}

// Actions returns active menu bindings.
func (t *Table) Actions() KeyActions {
	return t.actions
}

// SendKey sends an keyboard event (testing only!).
func (t *Table) SendKey(evt *tcell.EventKey) {
	t.keyboard(evt)
}

func (t *Table) keyboard(evt *tcell.EventKey) *tcell.EventKey {
	key := evt.Key()
	if key == tcell.KeyUp || key == tcell.KeyDown {
		return evt
	}

	if key == tcell.KeyRune {
		if t.SearchBuff().IsActive() {
			t.SearchBuff().Add(evt.Rune())
			t.ClearSelection()
			t.doUpdate(t.filtered())
			t.UpdateTitle()
			t.SelectFirstRow()
			return nil
		}
		key = asKey(evt)
	}

	if a, ok := t.actions[key]; ok {
		return a.Action(evt)
	}

	return evt
}

func (t *Table) Hints() model.MenuHints {
	return t.actions.Hints()
}

// GetFilteredData fetch filtered tabular data.
func (t *Table) GetFilteredData() render.TableData {
	return t.filtered()
}

// SetDecorateFn specifies the default row decorator.
func (t *Table) SetDecorateFn(f DecorateFunc) {
	t.decorateFn = f
}

// SetColorerFn specifies the default colorer.
func (t *Table) SetColorerFn(f render.ColorerFunc) {
	if f == nil {
		return
	}
	t.colorerFn = f
}

// SetSortCol sets in sort column index and order.
func (t *Table) SetSortCol(index, count int, asc bool) {
	t.sortCol.index, t.sortCol.colCount, t.sortCol.asc = index, count, asc
}

// Update table content.
func (t *Table) Update(data render.TableData) {
	t.Data = data
	if t.decorateFn != nil {
		data = t.decorateFn(data)
	}

	if t.cmdBuff.Empty() {
		t.doUpdate(data)
	} else {
		t.doUpdate(t.filtered())
	}
	t.UpdateTitle()
	t.updateSelection(true)
}

func (t *Table) doUpdate(data render.TableData) {
	if data.Namespace == render.AllNamespaces {
		t.actions[KeyShiftP] = NewKeyAction("Sort Namespace", t.SortColCmd(-2, true), false)
	} else {
		t.actions.Delete(KeyShiftP)
	}
	t.Clear()

	t.adjustSorter(data)

	var row int
	fg := config.AsColor(t.styles.Table().Header.FgColor)
	bg := config.AsColor(t.styles.Table().Header.BgColor)
	for col, h := range data.Header {
		t.AddHeaderCell(col, h)
		c := t.GetCell(0, col)
		c.SetBackgroundColor(bg)
		c.SetTextColor(fg)
	}
	row++

	data.RowEvents.Sort(data.Namespace, t.sortCol.index, t.sortCol.asc)

	pads := make(MaxyPad, len(data.Header))
	ComputeMaxColumns(pads, t.sortCol.index, data.Header, data.RowEvents)
	for i, r := range data.RowEvents {
		t.buildRow(data.Namespace, i+1, r, data.Header, pads)
	}
	t.updateSelection(false)
}

// SortColCmd designates a sorted column.
func (t *Table) SortColCmd(col int, asc bool) func(evt *tcell.EventKey) *tcell.EventKey {
	return func(evt *tcell.EventKey) *tcell.EventKey {
		var index int
		switch col {
		case -2:
			index = 0
		case -1:
			index = t.GetColumnCount() - 1
		default:
			index = t.NameColIndex() + col
		}
		t.sortCol.asc = !t.sortCol.asc
		if t.sortCol.index != index {
			t.sortCol.asc = asc
		}
		t.sortCol.index = index
		t.Refresh()

		return nil
	}
}

// SortInvertCmd reverses sorting order.
func (t *Table) SortInvertCmd(evt *tcell.EventKey) *tcell.EventKey {
	t.sortCol.asc = !t.sortCol.asc
	t.Refresh()

	return nil
}

func (t *Table) adjustSorter(data render.TableData) {
	// Going from namespace to non namespace or vice-versa?
	switch {
	case t.sortCol.colCount == 0:
	case len(data.Header) > t.sortCol.colCount:
		t.sortCol.index++
	case len(data.Header) < t.sortCol.colCount:
		t.sortCol.index--
	}
	t.sortCol.colCount = len(data.Header)
	if t.sortCol.index < 0 {
		t.sortCol.index = 0
	}
}

func (t *Table) buildRow(ns string, r int, re render.RowEvent, header render.HeaderRow, pads MaxyPad) {
	color := render.DefaultColorer
	if t.colorerFn != nil {
		color = t.colorerFn
	}
	marked := t.IsMarked(re.Row.ID)
	for col, field := range re.Row.Fields {
		if !re.Deltas.IsBlank() && !header.AgeCol(col) {
			field += Deltas(re.Deltas[col], field)
		}

		if header[col].Decorator != nil {
			field = header[col].Decorator(field)
		}

		if header[col].Align == tview.AlignLeft {
			field = formatCell(field, pads[col])
		}
		c := tview.NewTableCell(field)
		c.SetExpansion(1)
		c.SetAlign(header[col].Align)
		c.SetTextColor(color(ns, re))
		if marked {
			c.SetTextColor(config.AsColor(t.styles.Table().MarkColor))
		}
		t.SetCell(r, col, c)
	}
}

func (t *Table) ClearMarks() {
	t.marks = map[string]bool{}
	t.Refresh()
}

// Refresh update the table data.
func (t *Table) Refresh() {
	t.Update(t.Data)
}

// NameColIndex returns the index of the resource name column.
func (t *Table) NameColIndex() int {
	col := 0
	if t.Data.Namespace == render.AllNamespaces {
		col++
	}
	return col
}

// AddHeaderCell configures a table cell header.
func (t *Table) AddHeaderCell(col int, h render.Header) {
	c := tview.NewTableCell(sortIndicator(t.sortCol, t.styles.Table(), col, h.Name))
	c.SetExpansion(1)
	c.SetAlign(h.Align)
	t.SetCell(0, col, c)
}

func (t *Table) filtered() render.TableData {
	if t.cmdBuff.Empty() || IsLabelSelector(t.cmdBuff.String()) {
		return t.Data
	}

	q := t.cmdBuff.String()
	if isFuzzySelector(q) {
		return fuzzyFilter(q[2:], t.NameColIndex(), t.Data)
	}

	data, err := rxFilter(t.cmdBuff.String(), t.Data)
	if err != nil {
		log.Error().Err(errors.New("Invalid filter expression")).Msg("Regexp")
		t.cmdBuff.Clear()
		return t.Data
	}

	return data
}

// SearchBuff returns the associated command buffer.
func (t *Table) SearchBuff() *CmdBuff {
	return t.cmdBuff
}

// ShowDeleted marks row as deleted.
func (t *Table) ShowDeleted() {
	r, _ := t.GetSelection()
	cols := t.GetColumnCount()
	for x := 0; x < cols; x++ {
		t.GetCell(r, x).SetAttributes(tcell.AttrDim)
	}
}

// UpdateTitle refreshes the table title.
func (t *Table) UpdateTitle() {
	ns := t.Data.Namespace
	if ns == render.AllNamespaces {
		ns = render.NamespaceAll
	}
	t.SetTitle(styleTitle(t.GetRowCount(), ns, t.BaseTitle, t.Path, t.cmdBuff.String(), t.styles))
}
