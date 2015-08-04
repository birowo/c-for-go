package translator

import (
	"log"

	"github.com/cznic/c/internal/cc"
)

func (t *Translator) walkAST(unit *cc.TranslationUnit) ([]CDecl, error) {
	var declarations []CDecl

	next, cdecls, err := t.walkUnit(unit)
	if err != nil {
		return nil, err
	}
	declarations = append(declarations, cdecls...)

	for next != nil {
		next, cdecls, err = t.walkUnit(next)
		if err != nil {
			return nil, err
		}
		declarations = append(declarations, cdecls...)
	}
	return declarations, nil
}

func (t *Translator) walkUnit(unit *cc.TranslationUnit) (next *cc.TranslationUnit, declarations []CDecl, err error) {
	if unit == nil {
		return
	}
	if unit.ExternalDeclaration != nil {
		declarations, err = t.walkDeclaration(unit.ExternalDeclaration.Declaration)
	}
	next = unit.TranslationUnit
	return
}

func (t *Translator) walkDeclaration(declr *cc.Declaration) ([]CDecl, error) {
	// read type spec into a reference type declaration
	refDecl := &CDecl{Spec: &CTypeSpec{}}
	nextSpec := t.walkDeclarationSpec(declr.DeclarationSpecifiers, refDecl)
	for nextSpec != nil {
		nextSpec = t.walkDeclarationSpec(nextSpec, refDecl)
	}

	// prepare to collect declarations
	var declarations []CDecl

	decl := &CDecl{Spec: refDecl.Spec.Copy()}
	if declr.InitDeclaratorListOpt != nil {
		nextList := declr.InitDeclaratorListOpt.InitDeclaratorList
		for nextList != nil {
			decl = &CDecl{Spec: refDecl.Spec.Copy()}
			nextList = t.walkInitDeclaratorList(nextList, decl)
			declarations = append(declarations, *decl)
			t.valueMap[decl.Name] = decl.Value
		}
	}
	if len(declarations) == 0 {
		log.Println("EMPTY declaration:", refDecl)
	}

	return declarations, nil
}

func (t *Translator) walkInitializer(init *cc.Initializer, decl *CDecl) {
	switch init.Case {
	case 0:
		decl.Value = t.EvalAssignmentExpression(init.AssignmentExpression)
		decl.Expression = t.ExpandAssignmentExpression(init.AssignmentExpression)
	case 1, // '{' InitializerList '}'
		2: // '{' InitializerList ',' '}'
		unmanagedCaseWarn(init.Case, init.Token.Pos())
	}
}

func (t *Translator) walkInitDeclaratorList(declr *cc.InitDeclaratorList, decl *CDecl) (next *cc.InitDeclaratorList) {
	next = declr.InitDeclaratorList
	walkPointers(declr.InitDeclarator.Declarator.PointerOpt, decl)

	switch declr.InitDeclarator.Case {
	case 1: // Declarator '=' Initializer
		t.walkInitializer(declr.InitDeclarator.Initializer, decl)
	}

	nextDeclarator := t.walkDirectDeclarator(declr.InitDeclarator.Declarator.DirectDeclarator, decl)
	for nextDeclarator != nil {
		nextDeclarator = t.walkDirectDeclarator(nextDeclarator, decl)
	}
	return
}

func (t *Translator) walkParameterList(list *cc.ParameterList) (next *cc.ParameterList, decl *CDecl) {
	next = list.ParameterList
	declr := list.ParameterDeclaration
	switch declr.Case {
	case 0: // DeclarationSpecifiers Declarator
		decl = &CDecl{Spec: &CTypeSpec{}}
		nextDeclr := t.walkDeclarationSpec(declr.DeclarationSpecifiers, decl)
		for nextDeclr != nil {
			nextDeclr = t.walkDeclarationSpec(nextDeclr, decl)
		}

		walkPointers(declr.Declarator.PointerOpt, decl)
		nextDeclarator := t.walkDirectDeclarator(declr.Declarator.DirectDeclarator, decl)
		for nextDeclarator != nil {
			nextDeclarator = t.walkDirectDeclarator(nextDeclarator, decl)
		}
	case 1: // DeclarationSpecifiers AbstractDeclaratorOpt
		unmanagedCaseWarn(declr.Case, list.Token.Pos())
	}
	return
}

func (t *Translator) walkDirectDeclarator2(declr *cc.DirectDeclarator2, decl *CDecl) {
	switch declr.Case {
	case 0: // ParameterTypeList ')'
		spec := decl.Spec.(*CFunctionSpec)
		nextList, paramDecl := t.walkParameterList(declr.ParameterTypeList.ParameterList)
		if paramDecl != nil {
			spec.ParamList = append(spec.ParamList, *paramDecl)
		}
		for nextList != nil {
			nextList, paramDecl = t.walkParameterList(nextList)
			if paramDecl != nil {
				spec.ParamList = append(spec.ParamList, *paramDecl)
			}
		}
	case 1: // IdentifierListOpt ')'
		unmanagedCaseWarn(declr.Case, declr.Token.Pos())
	}
}

func (t *Translator) walkDirectDeclarator(declr *cc.DirectDeclarator, decl *CDecl) (next *cc.DirectDeclarator) {
	decl.Pos = declr.Token.Pos()
	switch declr.Case {
	case 0: // IDENTIFIER
		decl.Name = string(declr.Token.S())
	case 1: // '(' Declarator ')'
		walkPointers(declr.Declarator.PointerOpt, decl)
		next = declr.Declarator.DirectDeclarator
	case 2, // DirectDeclarator '[' TypeQualifierListOpt AssignmentExpressionOpt ']'
		3, // DirectDeclarator '[' "static" TypeQualifierListOpt AssignmentExpression ']'
		4, // DirectDeclarator '[' TypeQualifierList "static" AssignmentExpression ']'
		5: // DirectDeclarator '[' TypeQualifierListOpt '*' ']'
		assignmentExpr := declr.AssignmentExpression
		if declr.AssignmentExpressionOpt != nil {
			assignmentExpr = declr.AssignmentExpressionOpt.AssignmentExpression
		}
		val := t.ExpandAssignmentExpression(assignmentExpr)
		decl.AddArray(val)
		next = declr.DirectDeclarator
	case 6: // DirectDeclarator '(' DirectDeclarator2
		next = declr.DirectDeclarator
		decl.Spec = &CFunctionSpec{
			Returns: CDecl{
				Spec: decl.Spec,
				Pos:  decl.Pos,
			},
		}
		t.walkDirectDeclarator2(declr.DirectDeclarator2, decl)
	}
	return
}

func walkPointers(popt *cc.PointerOpt, decl *CDecl) {
	if popt == nil {
		return
	}
	nextPointer := popt.Pointer.Pointer
	pointers := uint8(1)
	for nextPointer != nil {
		nextPointer = nextPointer.Pointer
		pointers++
	}
	decl.SetPointers(pointers)
}

func (t *Translator) walkDeclarationSpec(declr *cc.DeclarationSpecifiers, decl *CDecl) (next *cc.DeclarationSpecifiers) {
	switch declr.Case {
	case 0: // StorageClassSpecifier DeclarationSpecifiersOpt
		next = declr.DeclarationSpecifiersOpt.DeclarationSpecifiers
	case 1: // TypeSpecifier DeclarationSpecifiersOpt
		t.walkTypeSpec(declr.TypeSpecifier, decl)
		if declr.DeclarationSpecifiersOpt != nil {
			next = declr.DeclarationSpecifiersOpt.DeclarationSpecifiers
		}
	case 2: // TypeQualifier DeclarationSpecifiersOpt
		if spec, ok := decl.Spec.(*CTypeSpec); ok {
			spec.Const = (declr.TypeQualifier.Case == 0)
		}
		if declr.DeclarationSpecifiersOpt != nil {
			next = declr.DeclarationSpecifiersOpt.DeclarationSpecifiers
		}
	case 3: // FunctionSpecifier DeclarationSpecifiersOpt
		unmanagedCaseWarn(declr.Case, declr.FunctionSpecifier.Token.Pos())
	}
	return
}

func (t *Translator) walkSpecifierQualifierList(declr *cc.SpecifierQualifierList, decl *CDecl) (next *cc.SpecifierQualifierList) {
	if declr.SpecifierQualifierListOpt != nil {
		next = declr.SpecifierQualifierListOpt.SpecifierQualifierList
	}
	switch declr.Case {
	case 0:
		t.walkTypeSpec(declr.TypeSpecifier, decl)
	case 1:
		if spec, ok := decl.Spec.(*CTypeSpec); ok {
			spec.Const = (declr.TypeQualifier.Case == 0)
		}
	}
	return
}

func (t *Translator) walkStructDeclarator(declr *cc.StructDeclarator, decl *CDecl) {
	switch declr.Case {
	case 0: // Declarator
		walkPointers(declr.Declarator.PointerOpt, decl)
		nextDeclr := declr.Declarator.DirectDeclarator
		for nextDeclr != nil {
			nextDeclr = t.walkDirectDeclarator(nextDeclr, decl)
		}
	case 1: // DeclaratorOpt ':' ConstantExpression
		unmanagedCaseWarn(declr.Case, declr.Token.Pos())
	}
}

func (t *Translator) walkStructDeclaration(declr *cc.StructDeclaration) []CDecl {
	refDecl := &CDecl{Spec: &CTypeSpec{}}
	nextList := declr.SpecifierQualifierList
	for nextList != nil {
		nextList = t.walkSpecifierQualifierList(nextList, refDecl)
	}

	// prepare to collect declarations
	var declarations []CDecl

	nextDeclaratorList := declr.StructDeclaratorListOpt.StructDeclaratorList
	for nextDeclaratorList != nil {
		decl := &CDecl{Spec: refDecl.Spec.Copy()}
		t.walkStructDeclarator(nextDeclaratorList.StructDeclarator, decl)
		nextDeclaratorList = nextDeclaratorList.StructDeclaratorList
		declarations = append(declarations, *decl)
	}

	return declarations
}

func (t *Translator) walkSUSpecifier(suSpec *cc.StructOrUnionSpecifier, decl *CDecl) {
	switch suSpec.Case {
	case 0: // StructOrUnionSpecifier0 '{' StructDeclarationList '}'
		walkSUSpecifier0(suSpec.StructOrUnionSpecifier0, decl)
		structSpec := decl.Spec.(*CStructSpec)
		nextList := suSpec.StructDeclarationList
		for nextList != nil {
			declarations := t.walkStructDeclaration(nextList.StructDeclaration)
			structSpec.Members = append(structSpec.Members, declarations...)
			nextList = nextList.StructDeclarationList
		}
	case 1: // StructOrUnion IDENTIFIER
		switch suSpec.StructOrUnion.Case {
		case 0: // struct
			decl.Spec = &CStructSpec{}
		case 1: // union
			decl.Spec = &CStructSpec{
				Union: true,
			}
		}
	}
}

func walkSUSpecifier0(suSpec *cc.StructOrUnionSpecifier0, decl *CDecl) {
	switch suSpec.StructOrUnion.Case {
	case 0: // struct
		decl.Spec = &CStructSpec{}
	case 1: // union
		decl.Spec = &CStructSpec{
			Union: true,
		}
	}
	if suSpec.IdentifierOpt != nil {
		decl.Spec.(*CStructSpec).Tag = string(suSpec.IdentifierOpt.Token.S())
	}
}

func (t *Translator) walkTypeSpec(typeSpec *cc.TypeSpecifier, decl *CDecl) {
	if typeSpec == nil {
		return
	}

	spec := decl.Spec.(*CTypeSpec)

	switch typeSpec.Case {
	case 0:
		spec.Base = "void"
	case 1:
		spec.Base = "char"
	case 2:
		spec.Short = true
	case 3:
		spec.Base = "int"
	case 4:
		if spec.Long {
			spec.Base = "long"
		} else {
			spec.Long = true
		}
	case 5:
		spec.Base = "float"
	case 6:
		spec.Base = "double"
	case 7: // IGNORE: signed
	case 8:
		spec.Unsigned = true
	case 9:
		spec.Base = "_Bool"
	case 10:
		spec.Base = "_Complex"
	case 11:
		t.walkSUSpecifier(typeSpec.StructOrUnionSpecifier, decl)
	case 12: // TODO: enums
	case 13:
		spec.Base = string(typeSpec.Token.S())
	}
}