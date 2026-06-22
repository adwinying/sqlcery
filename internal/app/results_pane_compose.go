package app

import (
	"fmt"
	"sort"
	"strings"

	"github.com/adwinying/sqlcery/internal/db"
)

type resultsPanePendingAction string

const (
	resultsPanePendingActionNone          resultsPanePendingAction = ""
	resultsPanePendingActionComposeInsert resultsPanePendingAction = "compose-insert"
	resultsPanePendingActionComposeUpdate resultsPanePendingAction = "compose-update"
	resultsPanePendingActionComposeDelete resultsPanePendingAction = "compose-delete"
	resultsPanePendingActionGotoTop       resultsPanePendingAction = "goto-top"
)

type resultsPaneComposeAction string

const (
	resultsPaneComposeActionInsert resultsPaneComposeAction = "INSERT"
	resultsPaneComposeActionUpdate resultsPaneComposeAction = "UPDATE"
	resultsPaneComposeActionDelete resultsPaneComposeAction = "DELETE"
)

type resultsPaneComposeResult struct {
	SQL             string
	Row             int
	UsedPrimaryKeys bool
	Source          db.TableRef
	Action          resultsPaneComposeAction
}

// statementExpander composes SQL Statement Expansion from a Result Set row.
// Construct with newStatementExpander so the dialect is injected once.
type statementExpander struct {
	dialect db.Dialect
}

func newStatementExpander(dialect db.Dialect) statementExpander {
	return statementExpander{dialect: dialect}
}

func (e statementExpander) composeInsert(latest *LatestResultContext, rowIndex int) (resultsPaneComposeResult, error) {
	return composeResultsPaneInsertSQL(e.dialect, latest, rowIndex)
}

func (e statementExpander) composeUpdate(latest *LatestResultContext, rowIndex int) (resultsPaneComposeResult, error) {
	return composeResultsPaneUpdateSQL(e.dialect, latest, rowIndex)
}

func (e statementExpander) composeDelete(latest *LatestResultContext, rowIndex int) (resultsPaneComposeResult, error) {
	return composeResultsPaneDeleteSQL(e.dialect, latest, rowIndex)
}

func (e statementExpander) composeInsertBulk(latest *LatestResultContext, rowIndices []int) (resultsPaneComposeBulkResult, error) {
	return composeResultsPaneInsertBulkSQL(e.dialect, latest, rowIndices)
}

func (e statementExpander) composeUpdateBulk(latest *LatestResultContext, rowIndices []int) (resultsPaneComposeBulkResult, error) {
	return composeResultsPaneUpdateBulkSQL(e.dialect, latest, rowIndices)
}

func (e statementExpander) composeDeleteBulk(latest *LatestResultContext, rowIndices []int) (resultsPaneComposeBulkResult, error) {
	return composeResultsPaneDeleteBulkSQL(e.dialect, latest, rowIndices)
}

func composeResultsPaneUpdateSQL(dialect db.Dialect, latest *LatestResultContext, rowIndex int) (resultsPaneComposeResult, error) {
	if latest == nil || latest.PreservedResult == nil {
		return resultsPaneComposeResult{}, fmt.Errorf("Results Pane has no rows to compose")
	}

	result := latest.PreservedResult
	if rowIndex < 0 || rowIndex >= len(result.Rows) {
		return resultsPaneComposeResult{}, fmt.Errorf("selected row is out of range")
	}

	source, ok := resultsPaneResultSource(latest)
	if !ok {
		return resultsPaneComposeResult{}, fmt.Errorf("result source table is unknown")
	}

	assignments := resultsPaneUpdateAssignments(result, result.Rows[rowIndex])
	predicates, usedPrimaryKeys := resultsPaneRowPredicates(result, result.Rows[rowIndex])
	if len(assignments) == 0 {
		return resultsPaneComposeResult{}, fmt.Errorf("selected row has no columns to update")
	}
	if !usedPrimaryKeys {
		return resultsPaneComposeResult{}, fmt.Errorf("selected row has no primary key columns; cannot compose a safe UPDATE")
	}
	if len(predicates) == 0 {
		return resultsPaneComposeResult{}, fmt.Errorf("selected row has no identifying predicate")
	}

	return resultsPaneComposeResult{
		SQL: db.NewComposer(dialect).Update(db.UpdateSpec{
			Table:       source,
			Assignments: assignments,
			Predicates:  predicates,
		}),
		Row:             rowIndex,
		UsedPrimaryKeys: usedPrimaryKeys,
		Source:          source,
		Action:          resultsPaneComposeActionUpdate,
	}, nil
}

func composeResultsPaneInsertSQL(dialect db.Dialect, latest *LatestResultContext, rowIndex int) (resultsPaneComposeResult, error) {
	if latest == nil || latest.PreservedResult == nil {
		return resultsPaneComposeResult{}, fmt.Errorf("Results Pane has no rows to compose")
	}

	result := latest.PreservedResult
	if rowIndex < 0 || rowIndex >= len(result.Rows) {
		return resultsPaneComposeResult{}, fmt.Errorf("selected row is out of range")
	}

	source, ok := resultsPaneResultSource(latest)
	if !ok {
		return resultsPaneComposeResult{}, fmt.Errorf("result source table is unknown")
	}

	columns := resultsPaneInsertColumnNames(result)
	if len(columns) == 0 {
		return resultsPaneComposeResult{}, fmt.Errorf("selected row has no columns to insert")
	}

	return resultsPaneComposeResult{
		SQL: db.NewComposer(dialect).Insert(db.InsertSpec{
			Table:   source,
			Columns: columns,
			Rows:    [][]db.ResultValue{resultsPaneRowValues(result, result.Rows[rowIndex])},
		}),
		Row:    rowIndex,
		Source: source,
		Action: resultsPaneComposeActionInsert,
	}, nil
}

func composeResultsPaneDeleteSQL(dialect db.Dialect, latest *LatestResultContext, rowIndex int) (resultsPaneComposeResult, error) {
	if latest == nil || latest.PreservedResult == nil {
		return resultsPaneComposeResult{}, fmt.Errorf("Results Pane has no rows to compose")
	}

	result := latest.PreservedResult
	if rowIndex < 0 || rowIndex >= len(result.Rows) {
		return resultsPaneComposeResult{}, fmt.Errorf("selected row is out of range")
	}

	source, ok := resultsPaneResultSource(latest)
	if !ok {
		return resultsPaneComposeResult{}, fmt.Errorf("result source table is unknown")
	}

	predicates, usedPrimaryKeys := resultsPaneRowPredicates(result, result.Rows[rowIndex])
	if len(predicates) == 0 {
		return resultsPaneComposeResult{}, fmt.Errorf("selected row has no identifying predicate")
	}

	return resultsPaneComposeResult{
		SQL: db.NewComposer(dialect).Delete(db.DeleteSpec{
			Table:      source,
			Predicates: predicates,
		}),
		Row:             rowIndex,
		UsedPrimaryKeys: usedPrimaryKeys,
		Source:          source,
		Action:          resultsPaneComposeActionDelete,
	}, nil
}

type resultsPaneComposeBulkResult struct {
	SQL    string
	Count  int
	Source db.TableRef
	Action resultsPaneComposeAction
}

func sortedRowIndices(rows []int) []int {
	sorted := append([]int(nil), rows...)
	sort.Ints(sorted)
	return sorted
}

func composeResultsPaneInsertBulkSQL(dialect db.Dialect, latest *LatestResultContext, rowIndices []int) (resultsPaneComposeBulkResult, error) {
	if latest == nil || latest.PreservedResult == nil {
		return resultsPaneComposeBulkResult{}, fmt.Errorf("Results Pane has no rows to compose")
	}

	result := latest.PreservedResult
	source, ok := resultsPaneResultSource(latest)
	if !ok {
		return resultsPaneComposeBulkResult{}, fmt.Errorf("result source table is unknown")
	}

	columns := resultsPaneInsertColumnNames(result)
	if len(columns) == 0 {
		return resultsPaneComposeBulkResult{}, fmt.Errorf("rows have no columns to insert")
	}

	indices := sortedRowIndices(rowIndices)
	rows := make([][]db.ResultValue, 0, len(indices))
	for _, idx := range indices {
		if idx < 0 || idx >= len(result.Rows) {
			return resultsPaneComposeBulkResult{}, fmt.Errorf("row %d is out of range", idx+1)
		}
		rows = append(rows, resultsPaneRowValues(result, result.Rows[idx]))
	}

	return resultsPaneComposeBulkResult{
		SQL: db.NewComposer(dialect).Insert(db.InsertSpec{
			Table:   source,
			Columns: columns,
			Rows:    rows,
		}),
		Count:  len(indices),
		Source: source,
		Action: resultsPaneComposeActionInsert,
	}, nil
}

func composeResultsPaneUpdateBulkSQL(dialect db.Dialect, latest *LatestResultContext, rowIndices []int) (resultsPaneComposeBulkResult, error) {
	if latest == nil || latest.PreservedResult == nil {
		return resultsPaneComposeBulkResult{}, fmt.Errorf("Results Pane has no rows to compose")
	}

	indices := sortedRowIndices(rowIndices)
	stmts := make([]string, 0, len(indices))
	var source db.TableRef
	for _, idx := range indices {
		r, err := composeResultsPaneUpdateSQL(dialect, latest, idx)
		if err != nil {
			return resultsPaneComposeBulkResult{}, fmt.Errorf("row %d: %w", idx+1, err)
		}
		stmts = append(stmts, r.SQL)
		source = r.Source
	}

	return resultsPaneComposeBulkResult{
		SQL:    strings.Join(stmts, "\n"),
		Count:  len(indices),
		Source: source,
		Action: resultsPaneComposeActionUpdate,
	}, nil
}

func composeResultsPaneDeleteBulkSQL(dialect db.Dialect, latest *LatestResultContext, rowIndices []int) (resultsPaneComposeBulkResult, error) {
	if latest == nil || latest.PreservedResult == nil {
		return resultsPaneComposeBulkResult{}, fmt.Errorf("Results Pane has no rows to compose")
	}

	result := latest.PreservedResult
	source, ok := resultsPaneResultSource(latest)
	if !ok {
		return resultsPaneComposeBulkResult{}, fmt.Errorf("result source table is unknown")
	}

	pkIndices := make([]int, 0)
	for i, col := range result.Columns {
		if col.PrimaryKey != nil {
			pkIndices = append(pkIndices, i)
		}
	}
	if len(pkIndices) > 1 {
		sortResultsPanePredicateIndices(result.Columns, pkIndices)
	}

	indices := sortedRowIndices(rowIndices)
	composer := db.NewComposer(dialect)

	var sql string
	if len(pkIndices) == 1 {
		values := make([]db.ResultValue, 0, len(indices))
		for _, idx := range indices {
			if idx < 0 || idx >= len(result.Rows) {
				return resultsPaneComposeBulkResult{}, fmt.Errorf("row %d is out of range", idx+1)
			}
			values = append(values, resultsPaneRowValue(result.Rows[idx], pkIndices[0]))
		}
		sql = composer.DeleteIn(db.DeleteInSpec{
			Table:  source,
			Column: resultsPaneColumnName(result.Columns, pkIndices[0]),
			Values: values,
		})
	} else {
		groups := make([][]db.ColumnValue, 0, len(indices))
		for _, idx := range indices {
			if idx < 0 || idx >= len(result.Rows) {
				return resultsPaneComposeBulkResult{}, fmt.Errorf("row %d is out of range", idx+1)
			}
			predicates, _ := resultsPaneRowPredicates(result, result.Rows[idx])
			groups = append(groups, predicates)
		}
		sql = composer.DeleteGroups(source, groups)
	}

	return resultsPaneComposeBulkResult{
		SQL:    sql,
		Count:  len(indices),
		Source: source,
		Action: resultsPaneComposeActionDelete,
	}, nil
}

func resultsPaneComposeBulkStatus(result resultsPaneComposeBulkResult) string {
	return fmt.Sprintf("Loaded %s for %d rows from %s into command mode.", result.Action, result.Count, displaySlashTableRef(result.Source))
}

func resultsPaneComposeStatus(result resultsPaneComposeResult) string {
	if result.Action == resultsPaneComposeActionInsert {
		return fmt.Sprintf("Loaded INSERT for row %d from %s into command mode.", result.Row+1, displaySlashTableRef(result.Source))
	}

	predicate := "visible column predicate"
	if result.UsedPrimaryKeys {
		predicate = "primary key predicate"
	}
	action := string(result.Action)
	if action == "" {
		action = string(resultsPaneComposeActionUpdate)
	}
	return fmt.Sprintf("Loaded %s for row %d from %s into command mode using %s.", action, result.Row+1, displaySlashTableRef(result.Source), predicate)
}

func resultsPaneInsertColumnNames(result *db.ResultSet) []string {
	columns := make([]string, 0, len(result.Columns))
	for i := range result.Columns {
		columns = append(columns, resultsPaneColumnName(result.Columns, i))
	}
	return columns
}

func resultsPaneRowValues(result *db.ResultSet, row db.ResultRow) []db.ResultValue {
	values := make([]db.ResultValue, 0, len(result.Columns))
	for i := range result.Columns {
		values = append(values, resultsPaneRowValue(row, i))
	}
	return values
}

func resultsPaneResultSource(latest *LatestResultContext) (db.TableRef, bool) {
	if latest == nil || latest.PreservedResult == nil {
		return db.TableRef{}, false
	}

	if latest.PreservedResult.Source != nil && strings.TrimSpace(latest.PreservedResult.Source.Name) != "" {
		return *latest.PreservedResult.Source, true
	}

	inferred := inferQuerySourceTable(latest.Statement)
	if inferred == nil || strings.TrimSpace(inferred.Name) == "" {
		return db.TableRef{}, false
	}
	return *inferred, true
}

func resultsPaneUpdateAssignments(result *db.ResultSet, row db.ResultRow) []db.ColumnValue {
	indices := make([]int, 0, len(result.Columns))
	for i, column := range result.Columns {
		if column.PrimaryKey != nil {
			continue
		}
		indices = append(indices, i)
	}
	if len(indices) == 0 {
		indices = make([]int, len(result.Columns))
		for i := range result.Columns {
			indices[i] = i
		}
	}

	return resultsPaneColumnValues(result, row, indices)
}

func resultsPaneRowPredicates(result *db.ResultSet, row db.ResultRow) ([]db.ColumnValue, bool) {
	primaryKeyIndices := make([]int, 0, len(result.Columns))
	for i, column := range result.Columns {
		if column.PrimaryKey != nil {
			primaryKeyIndices = append(primaryKeyIndices, i)
		}
	}
	if len(primaryKeyIndices) > 1 {
		sortResultsPanePredicateIndices(result.Columns, primaryKeyIndices)
	}
	if len(primaryKeyIndices) > 0 {
		return resultsPaneColumnValues(result, row, primaryKeyIndices), true
	}

	indices := make([]int, len(result.Columns))
	for i := range result.Columns {
		indices[i] = i
	}
	return resultsPaneColumnValues(result, row, indices), false
}

func sortResultsPanePredicateIndices(columns []db.ResultColumn, indices []int) {
	for i := 0; i < len(indices); i++ {
		for j := i + 1; j < len(indices); j++ {
			left := columns[indices[i]].PrimaryKey
			right := columns[indices[j]].PrimaryKey
			if left == nil || right == nil {
				continue
			}
			if left.Position <= right.Position {
				continue
			}
			indices[i], indices[j] = indices[j], indices[i]
		}
	}
}

func resultsPaneColumnValues(result *db.ResultSet, row db.ResultRow, indices []int) []db.ColumnValue {
	values := make([]db.ColumnValue, 0, len(indices))
	for _, index := range indices {
		values = append(values, db.ColumnValue{
			Column: resultsPaneColumnName(result.Columns, index),
			Value:  resultsPaneRowValue(row, index),
		})
	}
	return values
}

func resultsPaneColumnName(columns []db.ResultColumn, index int) string {
	if index >= 0 && index < len(columns) && strings.TrimSpace(columns[index].Name) != "" {
		return columns[index].Name
	}
	return fmt.Sprintf("column_%d", index+1)
}

func resultsPaneRowValue(row db.ResultRow, index int) db.ResultValue {
	if index >= 0 && index < len(row.Values) {
		return row.Values[index]
	}
	return db.ResultValue{Kind: db.ValueKindNull}
}

func inferQuerySourceTable(query string) *db.TableRef {
	tokens := currentQuerySourceTokens(sqlLex(query))
	if len(tokens) == 0 {
		return nil
	}
	if len(tokens) > 0 && tokens[len(tokens)-1].Symbol && tokens[len(tokens)-1].Text == ";" {
		tokens = tokens[:len(tokens)-1]
	}

	depth := 0
	for i := 0; i < len(tokens); i++ {
		if tokens[i].Symbol {
			switch tokens[i].Text {
			case "(":
				depth++
			case ")":
				if depth > 0 {
					depth--
				}
			}
			continue
		}
		if depth != 0 || !tokens[i].Keyword || !strings.EqualFold(tokens[i].Text, "FROM") {
			continue
		}

		ref, next, ok := parseQuerySourceTableReference(tokens, i+1)
		if !ok {
			return nil
		}
		if querySourceHasAdditionalTables(tokens, next) {
			return nil
		}
		return &ref
	}

	return nil
}

func currentQuerySourceTokens(tokens []sqlToken) []sqlToken {
	end := len(tokens)
	for end > 0 && tokens[end-1].Symbol && tokens[end-1].Text == ";" {
		end--
	}

	start := 0
	for i := 0; i < end; i++ {
		token := tokens[i]
		if token.Symbol && token.Text == ";" {
			start = i + 1
		}
	}
	return tokens[start:end]
}

func parseQuerySourceTableReference(tokens []sqlToken, start int) (db.TableRef, int, bool) {
	parts := make([]string, 0, 3)
	i := start
	for i < len(tokens) {
		if !tokens[i].Ident {
			break
		}
		parts = append(parts, tokens[i].Text)
		i++
		if i >= len(tokens) || !tokens[i].Symbol || tokens[i].Text != "." {
			break
		}
		i++
	}

	if len(parts) == 0 {
		return db.TableRef{}, start, false
	}

	ref := db.TableRef{}
	switch len(parts) {
	case 1:
		ref.Name = parts[0]
	case 2:
		ref.Namespace = parts[0]
		ref.Name = parts[1]
	default:
		ref.Catalog = parts[len(parts)-3]
		ref.Namespace = parts[len(parts)-2]
		ref.Name = parts[len(parts)-1]
	}
	return ref, i, true
}

func querySourceHasAdditionalTables(tokens []sqlToken, start int) bool {
	depth := 0
	for i := start; i < len(tokens); i++ {
		token := tokens[i]
		if token.Symbol {
			switch token.Text {
			case "(":
				depth++
			case ")":
				if depth > 0 {
					depth--
				}
			case ",":
				if depth == 0 {
					return true
				}
			case ";":
				if depth == 0 {
					return false
				}
			}
			continue
		}
		if depth != 0 || !token.Keyword {
			continue
		}

		switch strings.ToUpper(token.Text) {
		case "JOIN":
			return true
		case "WHERE", "GROUP", "HAVING", "ORDER", "LIMIT", "OFFSET", "RETURNING", "UNION", "EXCEPT", "INTERSECT", "WINDOW":
			return false
		}
	}
	return false
}
